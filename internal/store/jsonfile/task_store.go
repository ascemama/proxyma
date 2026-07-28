// Package jsonfile adapts Proxyma's JSON file store to typed domain stores.
package jsonfile

import (
	"fmt"
	"strings"

	"proxyma/internal/store"
	"proxyma/internal/tasks"
)

// TaskStore persists task records as one JSON file per task.
type TaskStore struct {
	files *store.FileStore
}

// NewTaskStore creates a JSON-file task store rooted at dir.
func NewTaskStore(dir string) *TaskStore {
	return WrapTaskStore(store.New(dir))
}

// WrapTaskStore adapts an existing file store for task records.
func WrapTaskStore(files *store.FileStore) *TaskStore {
	return &TaskStore{files: files}
}

// LoadAll loads every task record in the store.
func (s *TaskStore) LoadAll() ([]tasks.Task, error) {
	if s == nil || s.files == nil {
		return nil, fmt.Errorf("task store is not configured")
	}
	paths, err := s.files.ListJSONFiles()
	if err != nil {
		return nil, err
	}
	records := make([]tasks.Task, 0, len(paths))
	for _, path := range paths {
		var task tasks.Task
		if err := s.files.ReadJSONFile(path, &task); err != nil {
			return nil, err
		}
		records = append(records, task)
	}
	return records, nil
}

// Save writes the complete task record.
func (s *TaskStore) Save(task tasks.Task) error {
	if s == nil || s.files == nil {
		return fmt.Errorf("task store is not configured")
	}
	if strings.TrimSpace(task.TaskID) == "" {
		return fmt.Errorf("task_id is required")
	}
	key := recordKey(task.CreatedAt, task.TaskID)
	if key != task.TaskID {
		if err := s.files.DeleteJSON(task.TaskID); err != nil {
			return err
		}
	}
	return s.files.WriteJSON(key, task)
}
