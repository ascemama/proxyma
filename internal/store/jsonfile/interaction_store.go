package jsonfile

import (
	"fmt"
	"strings"

	"proxyma/internal/interaction"
	"proxyma/internal/store"
)

// InteractionStore persists interaction records as one JSON file per interaction.
type InteractionStore struct {
	files *store.FileStore
}

// NewInteractionStore creates a JSON-file interaction store rooted at dir.
func NewInteractionStore(dir string) *InteractionStore {
	return WrapInteractionStore(store.New(dir))
}

// WrapInteractionStore adapts an existing file store for interaction records.
func WrapInteractionStore(files *store.FileStore) *InteractionStore {
	return &InteractionStore{files: files}
}

// LoadAll loads every interaction record in the store.
func (s *InteractionStore) LoadAll() ([]interaction.Interaction, error) {
	if s == nil || s.files == nil {
		return nil, fmt.Errorf("interaction store is not configured")
	}
	paths, err := s.files.ListJSONFiles()
	if err != nil {
		return nil, err
	}
	records := make([]interaction.Interaction, 0, len(paths))
	for _, path := range paths {
		var record interaction.Interaction
		if err := s.files.ReadJSONFile(path, &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

// Save writes the complete interaction record.
func (s *InteractionStore) Save(record interaction.Interaction) error {
	if s == nil || s.files == nil {
		return fmt.Errorf("interaction store is not configured")
	}
	if strings.TrimSpace(record.InteractionID) == "" {
		return fmt.Errorf("interaction_id is required")
	}
	key := recordKey(record.CreatedAt, record.InteractionID)
	if key != record.InteractionID {
		if err := s.files.DeleteJSON(record.InteractionID); err != nil {
			return err
		}
	}
	return s.files.WriteJSON(key, record)
}
