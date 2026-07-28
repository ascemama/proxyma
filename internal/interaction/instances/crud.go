package instances

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"proxyma/internal/ids"
)

const defaultNtfySchema = "../../schemas/proxyma.interaction.ntfy.schema.json"

// CreateNtfy writes a new ntfy interaction instance with a generated UUID v4 id.
func CreateNtfy(root string, instance NtfyInstance, generateID func() (string, error)) (NtfyInstance, error) {
	if strings.TrimSpace(instance.ID) != "" {
		return NtfyInstance{}, fmt.Errorf("create ntfy interaction input must not include id")
	}
	if generateID == nil {
		generateID = ids.NewUUIDV4
	}
	id, err := generateID()
	if err != nil {
		return NtfyInstance{}, err
	}
	id = strings.TrimSpace(id)
	if !ids.IsUUIDV4(id) {
		return NtfyInstance{}, fmt.Errorf("generated id must be a UUID v4")
	}
	instance.ID = id
	instance = normalizeNtfyForStorage(instance)
	if err := validateNtfy(instance); err != nil {
		return NtfyInstance{}, err
	}
	path := ntfyRecordPath(root, id)
	if _, err := os.Stat(path); err == nil {
		return NtfyInstance{}, fmt.Errorf("ntfy interaction instance %q already exists", id)
	} else if !os.IsNotExist(err) {
		return NtfyInstance{}, err
	}
	if err := writeNtfyFile(path, instance); err != nil {
		return NtfyInstance{}, err
	}
	return instance, nil
}

// UpdateNtfy replaces an existing ntfy interaction instance without changing its id.
func UpdateNtfy(root string, id string, instance NtfyInstance) (NtfyInstance, error) {
	id = strings.TrimSpace(id)
	if !ids.IsUUIDV4(id) {
		return NtfyInstance{}, fmt.Errorf("id must be a UUID v4")
	}
	path, _, err := FindNtfy(root, id)
	if err != nil {
		return NtfyInstance{}, err
	}
	instance.ID = id
	instance = normalizeNtfyForStorage(instance)
	if err := validateNtfy(instance); err != nil {
		return NtfyInstance{}, err
	}
	if err := writeNtfyFile(path, instance); err != nil {
		return NtfyInstance{}, err
	}
	return instance, nil
}

// DeleteNtfy removes an existing ntfy interaction instance.
func DeleteNtfy(root string, id string) error {
	path, _, err := FindNtfy(root, id)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// FindNtfy returns the file path and instance for an ntfy interaction instance id.
func FindNtfy(root string, id string) (string, NtfyInstance, error) {
	id = strings.TrimSpace(id)
	paths, err := ntfyFiles(root)
	if err != nil {
		return "", NtfyInstance{}, err
	}
	for _, path := range paths {
		instance, err := loadNtfyFile(path)
		if err != nil {
			return "", NtfyInstance{}, err
		}
		if err := validateNtfy(instance); err != nil {
			return "", NtfyInstance{}, err
		}
		if strings.TrimSpace(instance.ID) == id {
			return path, instance, nil
		}
	}
	return "", NtfyInstance{}, os.ErrNotExist
}

func normalizeNtfyForStorage(instance NtfyInstance) NtfyInstance {
	instance.Schema = strings.TrimSpace(instance.Schema)
	if instance.Schema == "" {
		instance.Schema = defaultNtfySchema
	}
	instance.ID = strings.TrimSpace(instance.ID)
	instance.CreatorUserID = strings.TrimSpace(instance.CreatorUserID)
	instance.DisplayName = strings.TrimSpace(instance.DisplayName)
	instance.BaseURL = strings.TrimRight(strings.TrimSpace(instance.BaseURL), "/")
	instance.RequestTopic = strings.TrimSpace(instance.RequestTopic)
	instance.ResponseTopic = strings.TrimSpace(instance.ResponseTopic)
	instance.Token = strings.TrimSpace(instance.Token)
	instance.RespondedBy = strings.TrimSpace(instance.RespondedBy)
	return instance
}

func writeNtfyFile(path string, instance NtfyInstance) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw := ntfyFile{
		Schema:        instance.Schema,
		ID:            instance.ID,
		CreatorUserID: instance.CreatorUserID,
		DisplayName:   instance.DisplayName,
		Enabled:       instance.Enabled,
		BaseURL:       instance.BaseURL,
		RequestTopic:  instance.RequestTopic,
		ResponseTopic: instance.ResponseTopic,
		Token:         instance.Token,
		PollInterval:  instance.PollInterval.String(),
		PollTimeout:   instance.PollTimeout.String(),
		RespondedBy:   instance.RespondedBy,
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*.json")
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
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmpName, path)
}

func ntfyFiles(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, ProviderNtfy))
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
		paths = append(paths, filepath.Join(root, ProviderNtfy, entry.Name()))
	}
	return paths, nil
}

func ntfyRecordPath(root string, id string) string {
	return filepath.Join(root, ProviderNtfy, strings.TrimSpace(id)+".json")
}
