package service

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// writeAtomicJSON marshals value with indentation and replaces path via a
// synced temp file in the same directory, so published archives never expose a
// torn write. An empty path means in-memory mode: callers skip persistence.
func writeAtomicJSON(path, tempPrefix string, value any) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), tempPrefix)
	if err != nil {
		return err
	}
	tempName := file.Name()
	defer os.Remove(tempName)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}
