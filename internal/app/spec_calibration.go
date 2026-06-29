package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mauler/internal/runtimeprofile"
	"mauler/internal/settings"
)

// SpecCalibrationSample is one measured point in the n-max sweep. N==0 is the
// spec-off baseline; N>0 is draft-mtp with that draft window.
type SpecCalibrationSample struct {
	N         int     `json:"n"`
	TokPerSec float64 `json:"tok_per_sec"`
	Note      string  `json:"note,omitempty"`
}

// SpecCalibration is the cached result of an n-max sweep for one (model, ctx)
// combo. BestN is the fastest draft window; Speedup is BestN tok/s over baseline.
type SpecCalibration struct {
	Key       string                  `json:"key"`
	ModelID   string                  `json:"model_id"`
	BestN     int                     `json:"best_n"`
	TokPerSec float64                 `json:"tok_per_sec"`
	Baseline  float64                 `json:"baseline_tok_per_sec"`
	Speedup   float64                 `json:"speedup"`
	RanAt     string                  `json:"ran_at"`
	Samples   []SpecCalibrationSample `json:"samples"`
}

// specSweepN is the draft-window sweep. Unsloth/llama.cpp note the optimum is
// hardware-dependent; this covers the useful range without an unbounded search.
var specSweepN = []int{1, 2, 3, 4, 5}

// specCalibrationKey identifies a (model, context) combo. The GGUF filename in
// ModelID already encodes the quant, so model+ctx is a stable key on a given
// machine. (A GPU id could be added later for multi-GPU hosts.)
func specCalibrationKey(p settings.Profile) string {
	return fmt.Sprintf("%s|ctx=%d", strings.TrimSpace(p.ModelID), p.CtxTokens)
}

// pickBestSample returns the enabled (N>0) sample with the highest tok/s, plus a
// flag for whether any enabled sample was found. Pure, so it is unit-tested.
func pickBestSample(samples []SpecCalibrationSample) (SpecCalibrationSample, bool) {
	best := SpecCalibrationSample{}
	found := false
	for _, s := range samples {
		if s.N <= 0 || s.TokPerSec <= 0 {
			continue
		}
		if !found || s.TokPerSec > best.TokPerSec {
			best = s
			found = true
		}
	}
	return best, found
}

func baselineTokPerSec(samples []SpecCalibrationSample) float64 {
	for _, s := range samples {
		if s.N == 0 {
			return s.TokPerSec
		}
	}
	return 0
}

func specCalibrationsPath() (string, error) {
	dir, err := settings.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "spec-calibration.json"), nil
}

func loadSpecCalibrations() (map[string]SpecCalibration, error) {
	path, err := specCalibrationsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]SpecCalibration{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]SpecCalibration{}
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]SpecCalibration{}, nil // tolerate a corrupt cache
	}
	return out, nil
}

func saveSpecCalibration(cal SpecCalibration) error {
	all, err := loadSpecCalibrations()
	if err != nil {
		return err
	}
	all[cal.Key] = cal
	path, err := specCalibrationsPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// getSpecCalibration returns the cached calibration for a profile's (model, ctx).
func (a *App) getSpecCalibration(p settings.Profile) (SpecCalibration, bool) {
	all, err := loadSpecCalibrations()
	if err != nil {
		return SpecCalibration{}, false
	}
	cal, ok := all[specCalibrationKey(p)]
	return cal, ok
}

// CalibrateSpec sweeps spec_draft_n_max for the named profile, measures tok/s at
// each step (plus a spec-off baseline), caches the fastest, writes it onto the
// profile, and re-resolves the live plan. Heavy (one model reload per step) and
// user-triggered. Returns an error if the agent is busy or the model is not
// MTP-capable.
func (a *App) CalibrateSpec(profileName string) (SpecCalibration, error) {
	a.mu.Lock()
	busy := a.agentRunning
	p, ok := a.profiles.Profiles[profileName]
	a.mu.Unlock()
	if busy {
		return SpecCalibration{}, fmt.Errorf("stop the running agent before calibrating MTP")
	}
	if !ok {
		return SpecCalibration{}, fmt.Errorf("profile %q not found", profileName)
	}
	active := applyProvider(p, a.profiles)
	if !strings.EqualFold(strings.TrimSpace(active.Backend), "llamacpp") {
		return SpecCalibration{}, fmt.Errorf("MTP calibration only applies to the llama.cpp backend")
	}
	rp, rpOK := runtimeprofile.Match(active)
	info := a.probeModelMTP(active)
	capable := info.HasMTPHeads || runtimeprofile.LooksMTPModel(active) || (rpOK && rp.Supports.MTP)
	if !capable {
		return SpecCalibration{}, fmt.Errorf("model %q is not MTP-capable; nothing to calibrate", active.ModelID)
	}

	a.emit("mauler:spec_calibration", map[string]any{"status": "running", "step": "baseline"})

	var samples []SpecCalibrationSample
	// Baseline: speculative decoding off.
	base := active
	base.SpecType = ""
	base.SpecDraftNMax = 0
	if tps, err := a.measureSpecTokPerSec(base); err == nil && tps > 0 {
		samples = append(samples, SpecCalibrationSample{N: 0, TokPerSec: tps, Note: "spec off"})
	} else if err != nil {
		samples = append(samples, SpecCalibrationSample{N: 0, Note: "baseline failed: " + err.Error()})
	}

	for _, n := range specSweepN {
		a.emit("mauler:spec_calibration", map[string]any{"status": "running", "step": fmt.Sprintf("n=%d", n)})
		trial := active
		trial.SpecType = specTypeMTP
		trial.SpecDraftNMax = n
		tps, err := a.measureSpecTokPerSec(trial)
		if err != nil {
			samples = append(samples, SpecCalibrationSample{N: n, Note: "failed: " + err.Error()})
			continue
		}
		samples = append(samples, SpecCalibrationSample{N: n, TokPerSec: tps})
	}

	best, found := pickBestSample(samples)
	if !found {
		return SpecCalibration{}, fmt.Errorf("calibration produced no usable measurements (check the bridge/model)")
	}
	baseline := baselineTokPerSec(samples)
	speedup := 0.0
	if baseline > 0 {
		speedup = best.TokPerSec / baseline
	}
	cal := SpecCalibration{
		Key:       specCalibrationKey(active),
		ModelID:   active.ModelID,
		BestN:     best.N,
		TokPerSec: best.TokPerSec,
		Baseline:  baseline,
		Speedup:   speedup,
		Samples:   samples,
	}
	if err := saveSpecCalibration(cal); err != nil {
		a.emit("mauler:spec_calibration", map[string]any{"status": "error", "error": err.Error()})
		return cal, err
	}

	// Persist the tuned n onto the profile and re-resolve the live plan (which
	// also reads the cache, so the badge shows the speedup).
	a.mu.Lock()
	if stored, ok := a.profiles.Profiles[profileName]; ok {
		stored.SpecDraftNMax = best.N
		a.profiles.Profiles[profileName] = stored
		if a.cfg.ActiveProfile == profileName {
			a.loadedModelKey = ""
		}
		_ = settings.SaveProfiles(a.profiles)
	}
	a.mu.Unlock()
	a.autoApplySpec(profileName, active)

	a.emit("mauler:spec_calibration", map[string]any{
		"status":  "done",
		"best_n":  cal.BestN,
		"speedup": cal.Speedup,
	})
	return cal, nil
}

// GetSpecCalibration returns the cached calibration for the active profile. The
// zero value (empty Key) means "no calibration yet" — a single return keeps the
// Wails binding simple for the frontend.
func (a *App) GetSpecCalibration() SpecCalibration {
	a.mu.Lock()
	p, ok := a.profiles.Profiles[a.cfg.ActiveProfile]
	a.mu.Unlock()
	if !ok {
		return SpecCalibration{}
	}
	cal, found := a.getSpecCalibration(applyProvider(p, a.profiles))
	if !found {
		return SpecCalibration{}
	}
	return cal
}

// measureSpecTokPerSec force-reloads the model with the given profile's spec
// settings and measures average tok/s over a couple of short generations. It
// clears loadedModelKey first because modelLoadKey does not encode spec params,
// so two n values would otherwise reuse the same loaded server.
func (a *App) measureSpecTokPerSec(p settings.Profile) (float64, error) {
	client, err := buildClientForAgent(p)
	if err != nil {
		return 0, err
	}
	a.mu.Lock()
	a.loadedModelKey = ""
	a.mu.Unlock()

	loadCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := a.ensureModelLoaded(loadCtx, client, p); err != nil {
		return 0, err
	}

	cases := benchmarkCases(p)
	if len(cases) > 2 {
		cases = cases[:2] // General chat + Coding is enough signal, keeps it quick
	}
	var total float64
	var count int
	for _, spec := range cases {
		sc := runBenchmarkCase(client, spec)
		if sc.TokensPerSecond > 0 {
			total += sc.TokensPerSecond
			count++
		}
	}
	if count == 0 {
		return 0, fmt.Errorf("no tok/s measured")
	}
	return total / float64(count), nil
}
