package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"mauler/internal/settings"
)

const sessionLibraryMetadataVersion = 2

const (
	conversationModeAdaptive = "adaptive"
	conversationModeDirect   = "direct"
	conversationModeAgent    = "agent"
)

type sessionLibraryMetadata struct {
	Version int                 `json:"version"`
	Tags    map[string][]string `json:"tags"`
	Modes   map[string]string   `json:"modes,omitempty"`
}

func sessionLibraryMetadataPath() (string, error) {
	dir, err := settings.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session-library.json"), nil
}

func loadSessionLibraryMetadata() (sessionLibraryMetadata, error) {
	metadata := sessionLibraryMetadata{Version: sessionLibraryMetadataVersion, Tags: map[string][]string{}, Modes: map[string]string{}}
	path, err := sessionLibraryMetadataPath()
	if err != nil {
		return metadata, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return metadata, nil
	}
	if err != nil {
		return metadata, err
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return metadata, fmt.Errorf("read session library metadata: %w", err)
	}
	if metadata.Tags == nil {
		metadata.Tags = map[string][]string{}
	}
	if metadata.Modes == nil {
		metadata.Modes = map[string]string{}
	}
	for name, mode := range metadata.Modes {
		metadata.Modes[name] = normalizeConversationMode(mode)
	}
	metadata.Version = sessionLibraryMetadataVersion
	return metadata, nil
}

func saveSessionLibraryMetadata(metadata sessionLibraryMetadata) error {
	path, err := sessionLibraryMetadataPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	metadata.Version = sessionLibraryMetadataVersion
	if metadata.Tags == nil {
		metadata.Tags = map[string][]string{}
	}
	if metadata.Modes == nil {
		metadata.Modes = map[string]string{}
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o640); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err == nil {
		return nil
	}
	// Windows cannot replace an existing destination with os.Rename. Preserve
	// the old complete file until the replacement bytes are ready, then use the
	// same in-place fallback as repaired session transcripts.
	if err := os.WriteFile(path, data, 0o640); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	_ = os.Remove(temporary)
	return nil
}

func normalizeConversationMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case conversationModeDirect:
		return conversationModeDirect
	case conversationModeAgent:
		return conversationModeAgent
	default:
		return conversationModeAdaptive
	}
}

func (a *App) defaultConversationModeLocked() string {
	if a == nil || a.cfg == nil {
		return conversationModeAdaptive
	}
	return normalizeConversationMode(a.cfg.Agents.DefaultConversationMode)
}

func validateConversationMode(mode string) (string, error) {
	normalized := normalizeConversationMode(mode)
	if strings.TrimSpace(mode) == "" || strings.EqualFold(strings.TrimSpace(mode), normalized) {
		return normalized, nil
	}
	return "", fmt.Errorf("unsupported conversation mode %q", mode)
}

func normalizeSessionTags(tags []string) ([]string, error) {
	if len(tags) > 6 {
		return nil, fmt.Errorf("a conversation can have at most 6 tags")
	}
	out := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.Join(strings.Fields(strings.TrimSpace(tag)), " ")
		if tag == "" {
			continue
		}
		if utf8.RuneCountInString(tag) > 24 {
			return nil, fmt.Errorf("tag %q is longer than 24 characters", tag)
		}
		for _, r := range tag {
			if unicode.IsControl(r) {
				return nil, fmt.Errorf("tag %q contains a control character", tag)
			}
		}
		key := strings.ToLower(tag)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, tag)
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out, nil
}

func (a *App) sessionTagsSnapshot() (map[string][]string, error) {
	a.sessionLibraryMu.Lock()
	defer a.sessionLibraryMu.Unlock()
	metadata, err := loadSessionLibraryMetadata()
	if err != nil {
		return nil, err
	}
	snapshot := make(map[string][]string, len(metadata.Tags))
	for name, tags := range metadata.Tags {
		snapshot[name] = append([]string(nil), tags...)
	}
	return snapshot, nil
}

func (a *App) sessionModesSnapshot() (map[string]string, error) {
	a.sessionLibraryMu.Lock()
	defer a.sessionLibraryMu.Unlock()
	metadata, err := loadSessionLibraryMetadata()
	if err != nil {
		return nil, err
	}
	snapshot := make(map[string]string, len(metadata.Modes))
	for name, mode := range metadata.Modes {
		snapshot[name] = normalizeConversationMode(mode)
	}
	return snapshot, nil
}

// GetConversationMode returns the active desktop conversation preference.
// Adaptive remains the compatibility default for unsaved and legacy chats.
func (a *App) GetConversationMode() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return normalizeConversationMode(a.conversationMode)
}

// SetConversationMode updates the active conversation and, when sessionName
// names an existing saved chat, persists the choice with that conversation.
func (a *App) SetConversationMode(sessionName, mode string) error {
	mode, err := validateConversationMode(mode)
	if err != nil {
		return err
	}
	a.mu.Lock()
	if a.agentRunning || a.evalRunning {
		a.mu.Unlock()
		return fmt.Errorf("cannot change conversation mode while an agent run or eval is active")
	}
	a.mu.Unlock()
	if strings.TrimSpace(sessionName) != "" {
		if err := a.SetSavedConversationMode(sessionName, mode); err != nil {
			return err
		}
	}
	a.mu.Lock()
	if a.agentRunning || a.evalRunning {
		a.mu.Unlock()
		return fmt.Errorf("cannot change conversation mode while an agent run or eval is active")
	}
	a.conversationMode = mode
	a.mu.Unlock()
	return nil
}

// SetSavedConversationMode updates a saved conversation's preference without
// changing the active desktop chat. Conversation-library actions use this for
// background rows so they cannot silently alter the next active run.
func (a *App) SetSavedConversationMode(sessionName, mode string) error {
	mode, err := validateConversationMode(mode)
	if err != nil {
		return err
	}
	name, err := cleanSessionName(sessionName)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(mustSessionsDir(), name+".json")); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("saved conversation %q does not exist", name)
		}
		return err
	}
	return a.persistSessionConversationMode(name, mode)
}

func (a *App) persistSessionConversationMode(name, mode string) error {
	a.sessionLibraryMu.Lock()
	defer a.sessionLibraryMu.Unlock()
	metadata, err := loadSessionLibraryMetadata()
	if err != nil {
		return err
	}
	mode = normalizeConversationMode(mode)
	if mode == conversationModeAdaptive {
		delete(metadata.Modes, name)
	} else {
		metadata.Modes[name] = mode
	}
	return saveSessionLibraryMetadata(metadata)
}

// SetSessionTags replaces the user-owned labels for one saved conversation.
func (a *App) SetSessionTags(name string, tags []string) error {
	name, err := cleanSessionName(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(mustSessionsDir(), name+".json")); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("saved conversation %q does not exist", name)
		}
		return err
	}
	tags, err = normalizeSessionTags(tags)
	if err != nil {
		return err
	}
	a.sessionLibraryMu.Lock()
	defer a.sessionLibraryMu.Unlock()
	metadata, err := loadSessionLibraryMetadata()
	if err != nil {
		return err
	}
	if len(tags) == 0 {
		delete(metadata.Tags, name)
	} else {
		metadata.Tags[name] = tags
	}
	return saveSessionLibraryMetadata(metadata)
}

func (a *App) renameSessionTags(oldName, newName string) error {
	a.sessionLibraryMu.Lock()
	defer a.sessionLibraryMu.Unlock()
	metadata, err := loadSessionLibraryMetadata()
	if err != nil {
		return err
	}
	changed := false
	if tags, ok := metadata.Tags[oldName]; ok {
		delete(metadata.Tags, oldName)
		metadata.Tags[newName] = tags
		changed = true
	}
	if mode, ok := metadata.Modes[oldName]; ok {
		delete(metadata.Modes, oldName)
		metadata.Modes[newName] = mode
		changed = true
	}
	if !changed {
		return nil
	}
	return saveSessionLibraryMetadata(metadata)
}

func (a *App) deleteSessionTags(name string) error {
	a.sessionLibraryMu.Lock()
	defer a.sessionLibraryMu.Unlock()
	metadata, err := loadSessionLibraryMetadata()
	if err != nil {
		// Tags are optional. A damaged label file must not prevent deletion of
		// the authoritative transcript and recall record.
		return nil
	}
	_, hasTags := metadata.Tags[name]
	_, hasMode := metadata.Modes[name]
	if !hasTags && !hasMode {
		return nil
	}
	delete(metadata.Tags, name)
	delete(metadata.Modes, name)
	return saveSessionLibraryMetadata(metadata)
}
