package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WriteJSON atomically writes value as indented JSON under id.
func (s *FileStore) WriteJSON(id string, value any) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("record id is required")
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := s.RecordPath(id)
	tmp, err := os.CreateTemp(s.dir, ".tmp-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := tmpFileSync(tmpName); err != nil {
		return err
	}
	return replaceFile(tmpName, path)
}

func tmpFileSync(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

// DeleteJSON removes the JSON record for id if it exists.
func (s *FileStore) DeleteJSON(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("record id is required")
	}
	if err := os.Remove(s.RecordPath(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ReadJSON reads id into target.
func (s *FileStore) ReadJSON(id string, target any) error {
	data, err := os.ReadFile(s.RecordPath(id))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode %s: %w", s.RecordPath(id), err)
	}
	return nil
}

// ReadJSONFile reads path into target.
func (s *FileStore) ReadJSONFile(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

// ListJSONFiles returns all JSON files under the store directory.
func (s *FileStore) ListJSONFiles() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".json" {
			continue
		}
		paths = append(paths, filepath.Join(s.dir, entry.Name()))
	}
	return paths, nil
}
