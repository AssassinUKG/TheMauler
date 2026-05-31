package settings

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Save atomically writes settings to ~/.config/mauler/settings.toml.
func Save(s *Settings) error {
	return writeToml("settings.toml", s)
}

// SaveProfiles atomically writes profiles to ~/.config/mauler/profiles.toml.
func SaveProfiles(pf *ProfilesFile) error {
	return writeToml("profiles.toml", pf)
}

func writeToml(filename string, v any) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(v); err != nil {
		return fmt.Errorf("encode toml: %w", err)
	}

	tmp := filepath.Join(dir, filename+".tmp")
	if err := os.WriteFile(tmp, buf.Bytes(), 0o640); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	dst := filepath.Join(dir, filename)
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("rename to %s: %w", dst, err)
	}
	return nil
}
