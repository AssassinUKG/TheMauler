package app

import (
	"os"
	"path/filepath"
	"strings"

	"mauler/internal/settings"
)

// GetUserProfile returns the contents of the user profile document (~/.config/mauler/USER.md).
// Returns empty string (not an error) if no profile exists yet.
func (a *App) GetUserProfile() (string, error) {
	path, err := userProfilePath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SaveUserProfile writes content to ~/.config/mauler/USER.md.
func (a *App) SaveUserProfile(content string) error {
	path, err := userProfilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o640)
}

// loadUserProfile reads USER.md for injection into the system prompt.
// Returns empty string silently if the file doesn't exist.
func loadUserProfile() string {
	path, err := userProfilePath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func userProfilePath() (string, error) {
	dir, err := settings.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "USER.md"), nil
}
