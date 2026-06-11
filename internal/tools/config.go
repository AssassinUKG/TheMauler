package tools

import (
	"strings"
	"sync"

	"mauler/internal/settings"
)

// Tools resolve a few settings (shell backend/distro/user, protected paths) at call
// time. Reading settings.toml from disk on every shell/edit/write is both slow and can
// diverge from the config the app is actually running with. The app pushes a snapshot
// via SetConfigSnapshot whenever settings load or change; the accessors below read that
// snapshot and only fall back to settings.Load() when no snapshot has been set (e.g. in
// unit tests), preserving the previous behavior.
var (
	cfgSnapshotMu sync.RWMutex
	cfgSnapshot   *toolConfigSnapshot
)

type toolConfigSnapshot struct {
	shellBackend   string
	shellDistro    string
	shellUser      string
	protectedPaths []string
}

// SetConfigSnapshot caches the tool-relevant settings. Safe for concurrent use.
func SetConfigSnapshot(c settings.ToolsConfig) {
	paths := make([]string, len(c.ProtectedPaths))
	copy(paths, c.ProtectedPaths)
	cfgSnapshotMu.Lock()
	cfgSnapshot = &toolConfigSnapshot{
		shellBackend:   c.ShellBackend,
		shellDistro:    c.ShellDistro,
		shellUser:      c.ShellUser,
		protectedPaths: paths,
	}
	cfgSnapshotMu.Unlock()
}

func currentConfigSnapshot() *toolConfigSnapshot {
	cfgSnapshotMu.RLock()
	defer cfgSnapshotMu.RUnlock()
	return cfgSnapshot
}

func configuredShellBackend() string {
	if c := currentConfigSnapshot(); c != nil {
		return strings.TrimSpace(c.shellBackend)
	}
	cfg, _ := settings.Load()
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Tools.ShellBackend)
}

func configuredShellDistro() string {
	if c := currentConfigSnapshot(); c != nil {
		return strings.TrimSpace(c.shellDistro)
	}
	cfg, _ := settings.Load()
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Tools.ShellDistro)
}

func configuredShellUser() string {
	if c := currentConfigSnapshot(); c != nil {
		return strings.TrimSpace(c.shellUser)
	}
	cfg, _ := settings.Load()
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Tools.ShellUser)
}

func configuredProtectedPaths() []string {
	if c := currentConfigSnapshot(); c != nil {
		return c.protectedPaths
	}
	cfg, _ := settings.Load()
	if cfg == nil {
		return settings.DefaultSettings().Tools.ProtectedPaths
	}
	return cfg.Tools.ProtectedPaths
}
