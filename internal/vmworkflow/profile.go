package vmworkflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Profile struct {
	Schema         string   `json:"$schema,omitempty"`
	SchemaVersion  string   `json:"schema_version"`
	ProfileID      string   `json:"profile_id"`
	ProfileVersion string   `json:"profile_version"`
	Formats        []string `json:"formats"`
	CSV            struct {
		Encoding  string `json:"encoding"`
		Delimiter string `json:"delimiter"`
	} `json:"csv"`
	XLSX struct {
		Sheet     *string `json:"sheet"`
		HeaderRow int     `json:"header_row"`
	} `json:"xlsx"`
	Columns               map[string]ColumnMapping `json:"columns"`
	IdentityFields        []string                 `json:"identity_fields"`
	InstanceDiscriminator struct {
		Fallback     string   `json:"fallback"`
		SourceFields []string `json:"source_fields"`
	} `json:"instance_discriminator"`
	IgnoredSourceHeaders    []string `json:"ignored_source_headers"`
	PreserveUnmappedColumns bool     `json:"preserve_unmapped_columns"`
}

type ColumnMapping struct {
	SourceHeaders []string `json:"source_headers"`
	Required      bool     `json:"required"`
	Normalizer    string   `json:"normalizer,omitempty"`
}

func LoadProfile(path string) (Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, fmt.Errorf("read profile: %w", err)
	}
	schemaData, err := os.ReadFile(profileSchemaPath(path))
	if err != nil {
		return Profile{}, fmt.Errorf("read profile schema: %w", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(schemaData, &schema); err != nil || schema["$schema"] == nil {
		return Profile{}, fmt.Errorf("profile schema is invalid")
	}
	var profile Profile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&profile); err != nil {
		return Profile{}, fmt.Errorf("decode profile: %w", err)
	}
	if profile.SchemaVersion != "1" || strings.TrimSpace(profile.ProfileID) == "" || strings.TrimSpace(profile.ProfileVersion) == "" {
		return Profile{}, fmt.Errorf("profile requires schema_version 1, profile_id, and profile_version")
	}
	if len(profile.Formats) == 0 || len(profile.Columns) == 0 || len(profile.IdentityFields) == 0 || !profile.PreserveUnmappedColumns {
		return Profile{}, fmt.Errorf("profile must define formats, columns, identity_fields, and preserve_unmapped_columns=true")
	}
	for _, format := range profile.Formats {
		if format != "csv" && format != "xlsx" {
			return Profile{}, fmt.Errorf("unsupported profile format %q", format)
		}
	}
	if profile.CSV.Delimiter != "" && len([]rune(profile.CSV.Delimiter)) != 1 {
		return Profile{}, fmt.Errorf("csv delimiter must be one character")
	}
	if profile.XLSX.HeaderRow < 0 {
		return Profile{}, fmt.Errorf("xlsx header_row cannot be negative")
	}
	if profile.InstanceDiscriminator.Fallback != "" && profile.InstanceDiscriminator.Fallback != "normalized_evidence_sha256" {
		return Profile{}, fmt.Errorf("unsupported instance discriminator fallback %q", profile.InstanceDiscriminator.Fallback)
	}
	for _, field := range profile.IdentityFields {
		if _, ok := profile.Columns[field]; !ok {
			return Profile{}, fmt.Errorf("identity field %q has no column mapping", field)
		}
	}
	for field, mapping := range profile.Columns {
		if len(mapping.SourceHeaders) == 0 {
			return Profile{}, fmt.Errorf("column %q has no source header", field)
		}
		if strings.TrimSpace(field) == "" {
			return Profile{}, fmt.Errorf("profile contains an empty normalized field name")
		}
		if strings.HasPrefix(field, "cvss_v2_") || strings.HasPrefix(field, "cvss_v3_") {
			return Profile{}, fmt.Errorf("CVSS v2/v3 fields are not supported")
		}
	}
	return profile, nil
}

func findingKey(assetID, pluginID, protocol, port, instance string) string {
	value := strings.Join([]string{normalize(assetID), normalize(pluginID), normalize(protocol), normalize(port), normalize(instance)}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return "finding:" + hex.EncodeToString(sum[:])
}

func assetKey(assetID string) string {
	sum := sha256.Sum256([]byte(normalize(assetID)))
	return "asset:" + hex.EncodeToString(sum[:])
}

func normalize(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
}

func profileSchemaPath(profilePath string) string {
	return filepath.Join(filepath.Dir(profilePath), "..", "profile.schema.json")
}
