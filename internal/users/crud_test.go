package users

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"proxyma/internal/agents"
	"proxyma/internal/store"
	"proxyma/internal/targets"
)

const (
	testUserID   = "fcd2f3a9-d546-44f4-837b-0c7aa4219752"
	testAgentID  = "df9ec8bb-1a22-4181-a7c8-126ad553221d"
	testTargetID = "8dc60324-b3bb-4d19-9aa6-0e149e4938b8"
	testOtherID  = "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"
	testThirdID  = "7937dd2d-0b8a-4880-8afa-0ea60fd65a43"
	testPolicyID = "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"
)

func TestCreateUserGeneratesID(t *testing.T) {
	paths := testPaths(t)

	result, err := CreateUser(paths, testUser("Alice"), fixedID(testUserID))
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != testUserID || result.Resource != "users" || result.Action != "create" {
		t.Fatalf("result = %#v", result)
	}
	var stored User
	readJSON(t, result.Path, &stored)
	if stored.ID != testUserID || stored.Name != "Alice" || !stored.Admin || stored.PasswdHash == "" {
		t.Fatalf("stored = %#v", stored)
	}
}

func TestCreateUserRejectsProvidedID(t *testing.T) {
	user := testUser("Alice")
	user.ID = testUserID
	_, err := CreateUser(testPaths(t), user, fixedID(testOtherID))
	if err == nil || !strings.Contains(err.Error(), "must not include id") {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateUserRejectsProvidedID(t *testing.T) {
	paths := testPaths(t)
	writeUserRecord(t, paths, storedTestUser(testUserID, "Alice"))

	user := testUser("Alice Updated")
	user.ID = testOtherID
	_, err := UpdateUser(paths, testUserID, user)
	if err == nil || !strings.Contains(err.Error(), "must not include id") {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateUserPreservesExistingPasswordHash(t *testing.T) {
	paths := testPaths(t)
	writeUserRecord(t, paths, storedTestUser(testUserID, "Alice"))

	result, err := UpdateUser(paths, testUserID, testUserWithoutPassword("Alice Updated"))
	if err != nil {
		t.Fatal(err)
	}
	var stored User
	readJSON(t, result.Path, &stored)
	if stored.PasswdHash != validTestPasswdHash {
		t.Fatalf("passwd_hash = %q", stored.PasswdHash)
	}
	if stored.Name != "Alice Updated" || !stored.Admin {
		t.Fatalf("stored = %#v", stored)
	}
}

func TestUpdateUserPreservesExistingTheme(t *testing.T) {
	paths := testPaths(t)
	stored := storedTestUser(testUserID, "Alice")
	stored.Theme = ThemeDark
	writeUserRecord(t, paths, stored)

	result, err := UpdateUser(paths, testUserID, testUserWithoutPassword("Alice Updated"))
	if err != nil {
		t.Fatal(err)
	}
	var updated User
	readJSON(t, result.Path, &updated)
	if updated.Theme != ThemeDark {
		t.Fatalf("theme = %q, want %q", updated.Theme, ThemeDark)
	}
}

func TestUpdateUserThemePreservesLegacyRecordAndOtherFields(t *testing.T) {
	paths := testPaths(t)
	writeUserRecord(t, paths, storedTestUser(testUserID, "Alice"))
	idPath := filepath.Join(paths.UserDir, testUserID+".json")
	legacyPath := filepath.Join(paths.UserDir, "alice.json")
	if err := os.Rename(idPath, legacyPath); err != nil {
		t.Fatal(err)
	}

	result, err := UpdateUserTheme(paths, testUserID, ThemeDark)
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != legacyPath || result.Action != "update-theme" {
		t.Fatalf("result = %#v", result)
	}
	var stored User
	readJSON(t, legacyPath, &stored)
	if stored.Theme != ThemeDark || stored.Name != "Alice" || stored.PasswdHash != validTestPasswdHash {
		t.Fatalf("stored = %#v", stored)
	}
	if _, err := os.Stat(idPath); !os.IsNotExist(err) {
		t.Fatalf("unexpected ID-named user record: %v", err)
	}
}

func TestUpdateUserThemeRejectsUnsupportedTheme(t *testing.T) {
	paths := testPaths(t)
	writeUserRecord(t, paths, storedTestUser(testUserID, "Alice"))
	if _, err := UpdateUserTheme(paths, testUserID, "system"); err == nil || !strings.Contains(err.Error(), "light or dark") {
		t.Fatalf("err = %v", err)
	}
}

func TestResetUserPasswordUpdatesOnlyPasswordHash(t *testing.T) {
	paths := testPaths(t)
	writeUserRecord(t, paths, storedTestUser(testUserID, "Alice"))

	result, err := ResetUserPassword(paths, testUserID, []byte("new-password"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Resource != "users" || result.Action != "reset-password" || result.ID != testUserID {
		t.Fatalf("result = %#v", result)
	}
	var stored User
	readJSON(t, result.Path, &stored)
	if stored.ID != testUserID || stored.Name != "Alice" || !stored.Admin {
		t.Fatalf("stored = %#v", stored)
	}
	if stored.PasswdHash == validTestPasswdHash {
		t.Fatalf("passwd_hash was not changed")
	}
	if err := VerifyPassword([]byte("new-password"), stored.PasswdHash); err != nil {
		t.Fatal(err)
	}
}

func TestResetUserPasswordPreservesNonIDRecordName(t *testing.T) {
	paths := testPaths(t)
	writeUserRecord(t, paths, storedTestUser(testUserID, "Alice"))
	idPath := filepath.Join(paths.UserDir, testUserID+".json")
	legacyPath := filepath.Join(paths.UserDir, "alice.json")
	if err := os.Rename(idPath, legacyPath); err != nil {
		t.Fatal(err)
	}

	result, err := ResetUserPassword(paths, testUserID, []byte("new-password"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != legacyPath {
		t.Fatalf("result path = %q, want %q", result.Path, legacyPath)
	}
	if _, err := os.Stat(idPath); !os.IsNotExist(err) {
		t.Fatalf("unexpected ID-named user record: %v", err)
	}
	var stored User
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPassword([]byte("new-password"), stored.PasswdHash); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteUserRejectsReferencedUser(t *testing.T) {
	paths := testPaths(t)
	writeUserRecord(t, paths, storedTestUser(testUserID, "Alice"))
	writeAgentRecord(t, paths, agents.Agent{ID: testAgentID, AgentJWTSigningKey: "signing-key", Enabled: true, UserIDs: []string{testUserID}})
	mkdirAll(t, paths.TargetDir)

	_, err := DeleteUser(paths, testUserID)
	if err == nil || !strings.Contains(err.Error(), "referenced by agent") {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateAgentValidatesUserReference(t *testing.T) {
	paths := testPaths(t)
	mkdirAll(t, paths.UserDir)

	_, err := CreateAgent(paths, agents.Agent{AgentJWTSigningKey: "signing-key", Enabled: true, UserIDs: []string{testUserID}}, fixedID(testAgentID))
	if err == nil || !strings.Contains(err.Error(), "unknown user id") {
		t.Fatalf("err = %v", err)
	}

	writeUserRecord(t, paths, storedTestUser(testUserID, "Alice"))
	result, err := CreateAgent(paths, agents.Agent{AgentJWTSigningKey: "signing-key", Enabled: true, UserIDs: []string{testUserID}}, fixedID(testAgentID))
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != testAgentID || result.Resource != "agents" {
		t.Fatalf("result = %#v", result)
	}
}

func TestDeleteAgentRejectsReferencedTarget(t *testing.T) {
	paths := testPaths(t)
	writeUserRecord(t, paths, storedTestUser(testUserID, "Alice"))
	writeAgentRecord(t, paths, agents.Agent{ID: testAgentID, AgentJWTSigningKey: "signing-key", Enabled: true, UserIDs: []string{testUserID}})
	writeTargetRecord(t, paths, targets.Target{
		Transport: targets.TransportSSH,
		ID:        testTargetID,
		Hostname:  "target.example",
		Username:  "demo",
		PolicyIDs: []string{"baseline"},
		AgentIDs:  []string{testAgentID},
		UserIDs:   []string{testUserID},
		Auth:      targets.PasswordAuth("password"),
	})

	_, err := DeleteAgent(paths, testAgentID)
	if err == nil || !strings.Contains(err.Error(), "referenced by target") {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateTargetValidatesReferencesAndWrites(t *testing.T) {
	paths := testPaths(t)
	writePolicy(t, paths, testPolicyID)
	writeUserRecord(t, paths, storedTestUser(testUserID, "Alice"))
	writeAgentRecord(t, paths, agents.Agent{ID: testAgentID, AgentJWTSigningKey: "signing-key", Enabled: true, UserIDs: []string{testUserID}})

	result, err := CreateTarget(paths, targets.Target{
		Transport: targets.TransportSSH,
		OS:        targets.OSLinux,
		Hostname:  "target.example",
		Username:  "demo",
		PolicyIDs: []string{testPolicyID},
		AgentIDs:  []string{testAgentID},
		UserIDs:   []string{testUserID},
		Auth:      targets.PasswordAuth("password"),
	}, fixedID(testTargetID))
	if err != nil {
		t.Fatal(err)
	}
	var stored targets.Target
	readJSON(t, result.Path, &stored)
	if stored.ID != testTargetID || stored.Hostname != "target.example" {
		t.Fatalf("stored = %#v", stored)
	}
}

func TestCreateTargetAllowsDuplicateHost(t *testing.T) {
	paths := testPaths(t)
	writePolicy(t, paths, testPolicyID)
	writeUserRecord(t, paths, storedTestUser(testUserID, "Alice"))
	writeAgentRecord(t, paths, agents.Agent{ID: testAgentID, AgentJWTSigningKey: "signing-key", Enabled: true, UserIDs: []string{testUserID}})
	writeTargetRecord(t, paths, targets.Target{
		Transport: targets.TransportSSH,
		ID:        testTargetID,
		Hostname:  "target.example",
		Username:  "demo",
		PolicyIDs: []string{testPolicyID},
		AgentIDs:  []string{testAgentID},
		UserIDs:   []string{testUserID},
		Auth:      targets.PasswordAuth("password"),
	})

	result, err := CreateTarget(paths, targets.Target{
		Transport: targets.TransportSSH,
		OS:        targets.OSLinux,
		Hostname:  "target.example",
		Username:  "other-demo",
		PolicyIDs: []string{testPolicyID},
		AgentIDs:  []string{testAgentID},
		UserIDs:   []string{testUserID},
		Auth:      targets.PasswordAuth("password"),
	}, fixedID(testOtherID))
	if err != nil {
		t.Fatal(err)
	}
	var stored targets.Target
	readJSON(t, result.Path, &stored)
	if stored.ID != testOtherID || stored.Hostname != "target.example" || stored.Username != "other-demo" {
		t.Fatalf("stored = %#v", stored)
	}
}

func TestReadAgentsForUserFiltersOwnedAgents(t *testing.T) {
	paths := testPaths(t)
	writeAgentRecord(t, paths, agents.Agent{ID: testAgentID, AgentJWTSigningKey: "signing-key", Enabled: true, UserIDs: []string{testUserID}})
	writeAgentRecord(t, paths, agents.Agent{ID: testOtherID, AgentJWTSigningKey: "other-signing-key", Enabled: true, UserIDs: []string{testOtherID}})
	writeAgentRecord(t, paths, agents.Agent{ID: testThirdID, AgentJWTSigningKey: "third-signing-key", Enabled: true, UserIDs: []string{testUserID, testOtherID}})

	owned, err := ReadAgentsForUser(paths, " "+testUserID+" ")
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedAgentIDs(owned); strings.Join(got, ",") != strings.Join([]string{testThirdID, testAgentID}, ",") {
		t.Fatalf("owned agent ids = %#v", got)
	}

	none, err := ReadAgentsForUser(paths, "missing-user")
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("expected no agents, got %#v", none)
	}
}

func TestReadTargetsForUserFiltersOwnedTargets(t *testing.T) {
	paths := testPaths(t)
	writeTargetRecord(t, paths, targets.Target{
		Transport: targets.TransportSSH,
		ID:        testTargetID,
		Hostname:  "owned.example",
		Username:  "demo",
		PolicyIDs: []string{"baseline"},
		AgentIDs:  []string{testAgentID},
		UserIDs:   []string{testUserID},
		Auth:      targets.PasswordAuth("password"),
	})
	writeTargetRecord(t, paths, targets.Target{
		Transport: targets.TransportSSH,
		ID:        testOtherID,
		Hostname:  "other.example",
		Username:  "demo",
		PolicyIDs: []string{"baseline"},
		AgentIDs:  []string{testAgentID},
		UserIDs:   []string{testOtherID},
		Auth:      targets.PasswordAuth("password"),
	})
	writeTargetRecord(t, paths, targets.Target{
		Transport: targets.TransportSSH,
		ID:        testThirdID,
		Hostname:  "shared.example",
		Username:  "demo",
		PolicyIDs: []string{"baseline"},
		AgentIDs:  []string{testAgentID},
		UserIDs:   []string{testOtherID, testUserID},
		Auth:      targets.PasswordAuth("password"),
	})

	owned, err := ReadTargetsForUser(paths, testUserID)
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedTargetIDs(owned); strings.Join(got, ",") != strings.Join([]string{testThirdID, testTargetID}, ",") {
		t.Fatalf("owned target ids = %#v", got)
	}

	none, err := ReadTargetsForUser(paths, "missing-user")
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("expected no targets, got %#v", none)
	}
}

func fixedID(id string) IDGenerator {
	return func() (string, error) {
		return id, nil
	}
}

func testUser(name string) User {
	return User{Username: strings.ReplaceAll(NormalizeUsername(name), " ", "-"), Name: name, Admin: true, Status: StatusActive, PasswdHash: validTestPasswdHash}
}

func testUserWithoutPassword(name string) User {
	return User{Username: strings.ReplaceAll(NormalizeUsername(name), " ", "-"), Name: name, Admin: true, Status: StatusActive}
}

func storedTestUser(id string, name string) User {
	user := testUser(name)
	user.ID = id
	return user
}

func testPaths(t *testing.T) Paths {
	t.Helper()
	root := t.TempDir()
	return Paths{
		UserDir:   filepath.Join(root, "users"),
		AgentDir:  filepath.Join(root, "agents"),
		TargetDir: filepath.Join(root, "targets"),
		PolicyDir: filepath.Join(root, "policies"),
	}
}

func writeUserRecord(t *testing.T, paths Paths, user User) {
	t.Helper()
	if err := store.New(paths.UserDir).WriteJSON(user.ID, user); err != nil {
		t.Fatal(err)
	}
}

func writeAgentRecord(t *testing.T, paths Paths, agent agents.Agent) {
	t.Helper()
	if err := store.New(paths.AgentDir).WriteJSON(agent.ID, agent); err != nil {
		t.Fatal(err)
	}
}

func writeTargetRecord(t *testing.T, paths Paths, target targets.Target) {
	t.Helper()
	if target.Transport == "" {
		target.Transport = targets.TransportSSH
	}
	if target.OS == "" {
		if target.Transport == targets.TransportWinRM {
			target.OS = targets.OSWindows
		} else {
			target.OS = targets.OSLinux
		}
	}
	if err := store.New(paths.TargetDir).WriteJSON(target.ID, target); err != nil {
		t.Fatal(err)
	}
}

func writePolicy(t *testing.T, paths Paths, id string) {
	t.Helper()
	mkdirAll(t, paths.PolicyDir)
	body := `{"id":"` + id + `","name":"Test Policy","enabled":true,"rules":[{"id":"default","effect":"approval","always":true}]}`
	if err := os.WriteFile(filepath.Join(paths.PolicyDir, id+".json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func sortedAgentIDs(items []agents.Agent) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	sort.Strings(ids)
	return ids
}

func sortedTargetIDs(items []targets.Target) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	sort.Strings(ids)
	return ids
}
