// Package store provides file-backed JSON persistence helpers.
package store

import (
	"fmt"
	"path/filepath"
	"strings"
)

// FileStore stores JSON records beneath a base directory.
type FileStore struct {
	dir string
}

// New returns a file-backed JSON store.
func New(dir string) *FileStore {
	return &FileStore{dir: dir}
}

// Dir returns the store base directory.
func (s *FileStore) Dir() string {
	return s.dir
}

// RecordPath returns the JSON record path for id.
func (s *FileStore) RecordPath(id string) string {
	name := strings.ReplaceAll(id, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	return filepath.Join(s.dir, fmt.Sprintf("%s.json", name))
}
