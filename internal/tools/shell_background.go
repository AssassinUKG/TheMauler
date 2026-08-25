package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Background shell jobs for the standalone shell tool.
//
// The shared-terminal path in internal/app has its own pidfile-based job manager
// that only works on the bash/wsl backends. This manager covers the plain shell
// tool used in one-shot mode and on the PowerShell backend, so background=true is
// never silently downgraded to a blocking foreground run. It launches the command
// detached via os/exec, redirects output to a temp logfile, and tracks the real
// exit code through cmd.Wait() — no shell-specific "kill -0" probing required.

type bgJob struct {
	id      string
	command string
	logPath string
	started time.Time

	mu        sync.Mutex
	done      bool
	exitCode  int
	waitErr   error
	pollCount int
	lastPoll  time.Time
}

type bgManager struct {
	mu      sync.Mutex
	counter int
	jobs    map[string]*bgJob
}

var bgJobs = &bgManager{jobs: map[string]*bgJob{}}

// MaxConcurrentBackgroundJobs bounds how many background jobs may be tracked at
// once across the background launchers. It prevents a model that keeps starting
// long scans without ever polling them to completion from leaking processes, file
// descriptors, and temp logfiles without limit.
const MaxConcurrentBackgroundJobs = 24

// reapFinishedShellJobs drops jobs whose process has exited but were never polled
// to completion, freeing the map entry and removing the temp logfile. Safe to call
// at any time; running jobs are left untouched. Returns the number reaped.
func reapFinishedShellJobs() int {
	bgJobs.mu.Lock()
	var removed []*bgJob
	for id, job := range bgJobs.jobs {
		job.mu.Lock()
		done := job.done
		job.mu.Unlock()
		if done {
			removed = append(removed, job)
			delete(bgJobs.jobs, id)
		}
	}
	bgJobs.mu.Unlock()
	for _, job := range removed {
		_ = os.Remove(job.logPath)
	}
	return len(removed)
}

// ReapBackgroundShellJobs is the exported entry point the app calls at run-end to
// clean up finished-but-unpolled standalone jobs.
func ReapBackgroundShellJobs() int { return reapFinishedShellJobs() }

// SweepStaleJobLogs removes orphaned background-job logfiles from the temp dir —
// e.g. files left behind when the app was killed before a job was polled to done.
// Logfiles belonging to currently-tracked jobs are preserved. Returns the count.
func SweepStaleJobLogs() int {
	bgJobs.mu.Lock()
	active := make(map[string]bool, len(bgJobs.jobs))
	for _, job := range bgJobs.jobs {
		active[job.logPath] = true
	}
	bgJobs.mu.Unlock()
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "mauler_job_*.log"))
	if err != nil {
		return 0
	}
	removed := 0
	for _, path := range matches {
		if active[path] {
			continue
		}
		if os.Remove(path) == nil {
			removed++
		}
	}
	return removed
}

// BackgroundJobUpdate is a snapshot of a standalone background job for observers
// such as the UI. It mirrors the fields the frontend Jobs pane consumes so the
// non-shared-terminal job path (isolated mode, PowerShell backend) shows up there
// too — the shared-terminal path emits its own equivalent updates from the app.
type BackgroundJobUpdate struct {
	ID          string
	Command     string
	State       string // started | running | done
	Log         string
	Output      string
	ElapsedSec  int
	NextPollSec int
	Verbose     bool
	ExitCode    int
	Done        bool
}

// OnBackgroundJobUpdate, if set, is invoked when a standalone background job is
// started or polled. The app wires this to a Wails event so the Jobs tab mirrors
// jobs the agent runs outside the shared terminal. Left nil in headless/CLI use.
var OnBackgroundJobUpdate func(BackgroundJobUpdate)

func emitBackgroundJobUpdate(u BackgroundJobUpdate) {
	if OnBackgroundJobUpdate != nil {
		OnBackgroundJobUpdate(u)
	}
}

// runShellBackground handles the background/job params for the standalone shell
// tool. It returns handled=false when neither is set, so the caller falls through
// to a normal foreground run. Polling a job needs no command.
func runShellBackground(p shellParams) (string, bool, error) {
	if jobID := strings.TrimSpace(p.Job); jobID != "" {
		out, err := pollBackgroundShellJob(jobID, p.Verbose)
		return out, true, err
	}
	if p.Background {
		out, err := startBackgroundShellJob(p.Command, p.Verbose, p.Backend)
		return out, true, err
	}
	return "", false, nil
}

// startBackgroundShellJob launches command detached in the active backend and
// returns a handle the agent can poll. Output streams to a temp logfile so the
// job's stdout/stderr survives across polls.
func startBackgroundShellJob(command string, verbose bool, forcedBackend string) (string, error) {
	command, err := PrepareShellCommand(command)
	if err != nil {
		return "", fmt.Errorf("shell: %w", err)
	}

	// Free any finished-but-unpolled jobs, then refuse to start more than the cap
	// so an un-polled job pileup can't exhaust processes/fds/temp files.
	reapFinishedShellJobs()
	bgJobs.mu.Lock()
	running := len(bgJobs.jobs)
	bgJobs.mu.Unlock()
	if running >= MaxConcurrentBackgroundJobs {
		return "", fmt.Errorf("shell: too many background jobs running (%d/%d); poll existing jobs to completion before starting more", running, MaxConcurrentBackgroundJobs)
	}

	backend := detectShellBackend(forcedBackend)
	if err := validateShellBackend(backend); err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		if inner, ok := unwrapNestedPowerShellCommand(command); ok {
			command = inner
			backend = "powershell"
		}
	}
	distro := ""
	if backend == "wsl" {
		distro = activeWSLDistro()
	}
	wd, _ := os.Getwd()

	bgJobs.mu.Lock()
	bgJobs.counter++
	id := fmt.Sprintf("j%d", bgJobs.counter)
	bgJobs.mu.Unlock()

	logPath := filepath.Join(os.TempDir(), "mauler_job_"+id+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return "", fmt.Errorf("shell: failed to create job log: %w", err)
	}

	cmd, err := shellCommand(context.Background(), backend, distro, command, wd)
	if err != nil {
		logFile.Close()
		return "", err
	}
	applyHiddenWindow(cmd)
	if backend == "wsl" {
		// wsl.exe emits UTF-16LE stdout by default. Since this path merges stdout
		// and stderr into one logfile, a UTF-16 stdout mixed with an ASCII stderr
		// line defeats the UTF-16 heuristic in decodeCommandOutput. WSL_UTF8=1 makes
		// wsl.exe emit UTF-8 instead, keeping the merged log uniformly decodable.
		cmd.Env = append(os.Environ(), "WSL_UTF8=1")
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return "", fmt.Errorf("shell: failed to launch background job: %w", err)
	}

	now := time.Now()
	job := &bgJob{id: id, command: command, logPath: logPath, started: now, lastPoll: now}
	bgJobs.mu.Lock()
	bgJobs.jobs[id] = job
	bgJobs.mu.Unlock()

	go func() {
		waitErr := cmd.Wait()
		logFile.Close()
		job.mu.Lock()
		job.done = true
		job.exitCode = exitCodeFromWaitErr(waitErr)
		job.waitErr = waitErr
		job.mu.Unlock()
	}()

	emitBackgroundJobUpdate(BackgroundJobUpdate{
		ID: id, Command: command, State: "started", Log: logPath,
		ElapsedSec: 0, NextPollSec: int(backgroundJobPollInterval(0).Seconds()), Verbose: verbose,
	})

	var sb strings.Builder
	sb.WriteString(shellResultContract("shell", backend, "started", -1, wd, id, "poll shell job after backoff", ""))
	fmt.Fprintf(&sb, "Started background job %s on the %s backend: %s\n", id, backend, command)
	fmt.Fprintf(&sb, "Output is streaming to %s.\n", logPath)
	fmt.Fprintf(&sb, "Poll after %s with {\"job\":\"%s\"}; use {\"job\":\"%s\",\"verbose\":true} for a larger output tail.",
		backgroundJobPollInterval(0), id, id)
	if verbose {
		fmt.Fprintf(&sb, "\n[background job %s verbose] log=%s backoff=1s,2s,3s,5s,8s,13s,30s", id, logPath)
	}
	return sb.String(), nil
}

// pollBackgroundShellJob reports a job's running/done state plus the tail of its
// output. A finished job is forgotten after it is reported once.
func pollBackgroundShellJob(id string, verbose bool) (string, error) {
	id = strings.TrimSpace(id)
	bgJobs.mu.Lock()
	job := bgJobs.jobs[id]
	bgJobs.mu.Unlock()
	if job == nil {
		return "", fmt.Errorf("shell: no background job %q (unknown id, or it already finished and was cleared)", id)
	}

	now := time.Now()
	job.mu.Lock()
	if !job.lastPoll.IsZero() {
		interval := backgroundJobPollInterval(job.pollCount)
		if elapsed := now.Sub(job.lastPoll); elapsed < interval {
			wait := (interval - elapsed).Round(time.Second)
			if wait < time.Second {
				wait = time.Second
			}
			state := "running"
			if job.done {
				state = "done"
			}
			elapsedTotal := now.Sub(job.started).Round(time.Second)
			job.mu.Unlock()
			body := fmt.Sprintf("[background job %s: %s, %s elapsed] poll skipped: too early; wait %s before polling again. Backoff schedule: 1s, 2s, 3s, 5s, 8s, 13s, then 30s.",
				id, state, elapsedTotal, wait)
			return shellResultContract("shell", "background", state, -1, "", id, "wait then poll shell job", body), nil
		}
	}
	done := job.done
	exitCode := job.exitCode
	job.lastPoll = now
	job.pollCount++
	pollCount := job.pollCount
	job.mu.Unlock()

	tailBytes := int64(12000)
	if verbose {
		tailBytes = 65000
	}
	text := tailFile(job.logPath, tailBytes)
	if strings.TrimSpace(text) == "" {
		text = "(no output captured yet)"
	}

	elapsed := time.Since(job.started).Round(time.Second)
	var footer string
	nextPollSec := 0
	if done {
		bgJobs.mu.Lock()
		delete(bgJobs.jobs, id)
		bgJobs.mu.Unlock()
		_ = os.Remove(job.logPath)
		footer = fmt.Sprintf("[background job %s: done, exit %d, %s elapsed] (job cleared)", id, exitCode, elapsed)
	} else {
		nextPoll := backgroundJobPollInterval(pollCount)
		nextPollSec = int(nextPoll.Seconds())
		footer = fmt.Sprintf("[background job %s: running, %s elapsed] (next poll in %s; then use {\"job\":\"%s\"})", id, elapsed, nextPoll, id)
		if verbose {
			footer += fmt.Sprintf(" [tail=%d bytes log=%s command=%q]", tailBytes, job.logPath, job.command)
		}
	}
	state := "running"
	if done {
		state = "done"
	}
	emitBackgroundJobUpdate(BackgroundJobUpdate{
		ID: id, Command: job.command, State: state, Log: job.logPath, Output: text,
		ElapsedSec: int(elapsed.Seconds()), NextPollSec: nextPollSec, Verbose: verbose,
		ExitCode: exitCode, Done: done,
	})
	nextTool := "poll shell job after backoff"
	if done {
		nextTool = "proceed"
		if exitCode != 0 {
			nextTool = "inspect error or change command"
		}
	}
	return shellResultContract("shell", "background", state, exitCode, "", id, nextTool, text+"\n\n"+footer), nil
}

func backgroundJobPollInterval(polls int) time.Duration {
	switch {
	case polls <= 0:
		return time.Second
	case polls == 1:
		return 2 * time.Second
	case polls == 2:
		return 3 * time.Second
	case polls == 3:
		return 5 * time.Second
	case polls == 4:
		return 8 * time.Second
	case polls <= 7:
		return 13 * time.Second
	default:
		return 30 * time.Second
	}
}

// tailFile returns the last maxBytes of the file decoded as text, tolerating
// PowerShell's UTF-16 output the same way the foreground path does.
func tailFile(path string, maxBytes int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	size := info.Size()
	if size > maxBytes {
		if _, err := f.Seek(size-maxBytes, 0); err != nil {
			return ""
		}
	}
	data := make([]byte, 0, maxBytes)
	buf := make([]byte, 32*1024)
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if readErr != nil {
			break
		}
	}
	out := decodeCommandOutput(data)
	if size > maxBytes {
		// We sliced into the middle of the stream; drop the partial first line.
		if idx := strings.IndexByte(out, '\n'); idx >= 0 {
			out = out[idx+1:]
		}
		out = "[...truncated to last " + fmt.Sprintf("%d", maxBytes) + " bytes...]\n" + out
	}
	return strings.TrimRight(out, "\r\n")
}

func exitCodeFromWaitErr(err error) int {
	if err == nil {
		return 0
	}
	type exitCoder interface{ ExitCode() int }
	if ec, ok := err.(exitCoder); ok {
		return ec.ExitCode()
	}
	return -1
}
