package users

import (
	"fmt"
	"strings"

	"proxyma/internal/ids"
	"proxyma/internal/llm"
)

func ReadLLMProfiles(paths Paths) ([]llm.Profile, error) {
	profiles, diagnostics, err := llm.LoadDir(paths.LLMProfileDir)
	if err != nil {
		return nil, err
	}
	if len(diagnostics) > 0 {
		return nil, fmt.Errorf("LLM profiles invalid: %s: %s", diagnostics[0].Path, diagnostics[0].Message)
	}
	return profiles, nil
}

func CreateLLMProfile(paths Paths, profile llm.Profile, generateID IDGenerator) (MutationResult, error) {
	if strings.TrimSpace(profile.ID) != "" {
		return MutationResult{}, fmt.Errorf("LLM profile create input must not include id")
	}
	id, err := newID(generateID)
	if err != nil {
		return MutationResult{}, err
	}
	profile.ID = id
	if profile.BaseURL == "" {
		profile.BaseURL = llm.DefaultBaseURL(profile.Provider)
	}
	if err := llm.ValidateProfile(&profile); err != nil {
		return MutationResult{}, err
	}
	existingProfiles, err := ReadLLMProfiles(paths)
	if err != nil {
		return MutationResult{}, err
	}
	for _, existing := range existingProfiles {
		if existing.ID == profile.ID {
			return MutationResult{}, fmt.Errorf("LLM profile %q already exists", profile.ID)
		}
	}
	if err := llm.Write(paths.LLMProfileDir, profile); err != nil {
		return MutationResult{}, err
	}
	return result("llm-profiles", "create", id, paths.LLMProfileDir+"/"+id+".json"), nil
}

func UpdateLLMProfile(paths Paths, id string, profile llm.Profile) (MutationResult, error) {
	id = strings.TrimSpace(id)
	if !ids.IsUUIDV4(id) {
		return MutationResult{}, fmt.Errorf("LLM profile id must be a UUID v4")
	}
	existing, err := findLLMProfile(paths, id)
	if err != nil {
		return MutationResult{}, err
	}
	profile.ID = id
	profile.OwnerUserID = existing.OwnerUserID
	if strings.TrimSpace(profile.APIKey) == "" {
		profile.APIKey = existing.APIKey
	}
	if profile.BaseURL == "" {
		profile.BaseURL = llm.DefaultBaseURL(profile.Provider)
	}
	if err := llm.ValidateProfile(&profile); err != nil {
		return MutationResult{}, err
	}
	if err := llm.Write(paths.LLMProfileDir, profile); err != nil {
		return MutationResult{}, err
	}
	return result("llm-profiles", "update", id, paths.LLMProfileDir+"/"+id+".json"), nil
}

func DeleteLLMProfile(paths Paths, id string) (MutationResult, error) {
	id = strings.TrimSpace(id)
	if _, err := findLLMProfile(paths, id); err != nil {
		return MutationResult{}, err
	}
	policies, err := ReadPolicies(paths)
	if err != nil {
		return MutationResult{}, err
	}
	for _, item := range policies {
		if item.LLM != nil && item.LLM.ProfileID == id {
			return MutationResult{}, fmt.Errorf("LLM profile %q is referenced by policy %q", id, item.ID)
		}
	}
	if err := llm.Delete(paths.LLMProfileDir, id); err != nil {
		return MutationResult{}, err
	}
	return result("llm-profiles", "delete", id, paths.LLMProfileDir+"/"+id+".json"), nil
}

func CanModifyLLMProfile(profile llm.Profile, userID string, admin bool) bool {
	return admin || strings.TrimSpace(profile.OwnerUserID) == strings.TrimSpace(userID)
}

func CanUseLLMProfile(profile llm.Profile, userID string, admin bool) bool {
	return admin || strings.TrimSpace(profile.OwnerUserID) == strings.TrimSpace(userID)
}

func findLLMProfile(paths Paths, id string) (llm.Profile, error) {
	profiles, err := ReadLLMProfiles(paths)
	if err != nil {
		return llm.Profile{}, err
	}
	for _, profile := range profiles {
		if profile.ID == id {
			return profile, nil
		}
	}
	return llm.Profile{}, fmt.Errorf("LLM profile %q does not exist", id)
}
