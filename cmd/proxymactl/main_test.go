package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"proxyma/internal/users"
)

func TestRunUsersCreateUpdateDelete(t *testing.T) {
	root := t.TempDir()
	userDir := filepath.Join(root, "users")
	inputPath := filepath.Join(root, "user.json")
	if err := os.WriteFile(inputPath, []byte(`{"username":"alice","name":"Alice","admin":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(append(testGlobalArgs(root), "users", "create", "--file", inputPath, "--password-stdin"), &stdout, &stderr, strings.NewReader("initial-password\n"), emptyEnv)
	if code != 0 {
		t.Fatalf("code = %d stderr = %s", code, stderr.String())
	}
	var created users.MutationResult
	if err := json.Unmarshal(stdout.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Resource != "users" || created.Action != "create" || created.ID == "" {
		t.Fatalf("created = %#v", created)
	}
	if _, err := os.Stat(filepath.Join(userDir, created.ID+".json")); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	code = run(append(testGlobalArgs(root), "users", "update", created.ID, "--file", "-"), &stdout, &stderr, strings.NewReader(`{"username":"alice","name":"Alice Updated","admin":false}`), emptyEnv)
	if code != 0 {
		t.Fatalf("code = %d stderr = %s", code, stderr.String())
	}
	var stored users.User
	readJSONFile(t, filepath.Join(userDir, created.ID+".json"), &stored)
	if stored.ID != created.ID || stored.Name != "Alice Updated" || stored.Admin || stored.PasswdHash == "" {
		t.Fatalf("stored = %#v", stored)
	}

	stdout.Reset()
	stderr.Reset()
	code = run(append(testGlobalArgs(root), "users", "delete", created.ID), &stdout, &stderr, strings.NewReader(""), emptyEnv)
	if code != 0 {
		t.Fatalf("code = %d stderr = %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(userDir, created.ID+".json")); !os.IsNotExist(err) {
		t.Fatalf("deleted user still exists or unexpected stat error: %v", err)
	}
}

func TestRunCreateRejectsInputID(t *testing.T) {
	root := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(append(testGlobalArgs(root), "users", "create", "--file", "-"), &stdout, &stderr, strings.NewReader(`{"id":"fcd2f3a9-d546-44f4-837b-0c7aa4219752","username":"alice","name":"Alice","admin":true}`), emptyEnv)
	if code == 0 {
		t.Fatalf("expected failure stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "must not include id") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestRunUsersResetPassword(t *testing.T) {
	root := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(append(testGlobalArgs(root), "users", "create", "--file", "-"), &stdout, &stderr, strings.NewReader(`{"username":"alice","name":"Alice","admin":true,"passwd_hash":"`+validTestPasswdHash+`"}`), emptyEnv)
	if code != 0 {
		t.Fatalf("code = %d stderr = %s", code, stderr.String())
	}
	var created users.MutationResult
	if err := json.Unmarshal(stdout.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	code = run(append(testGlobalArgs(root), "users", "reset-password", created.ID, "--password-stdin"), &stdout, &stderr, strings.NewReader("new-password\n"), emptyEnv)
	if code != 0 {
		t.Fatalf("code = %d stderr = %s", code, stderr.String())
	}
	var reset users.MutationResult
	if err := json.Unmarshal(stdout.Bytes(), &reset); err != nil {
		t.Fatal(err)
	}
	if reset.Resource != "users" || reset.Action != "reset-password" || reset.ID != created.ID {
		t.Fatalf("reset = %#v", reset)
	}
	if strings.Contains(stdout.String(), "new-password") || strings.Contains(stderr.String(), "new-password") {
		t.Fatalf("password leaked stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	var stored users.User
	readJSONFile(t, filepath.Join(root, "users", created.ID+".json"), &stored)
	if stored.PasswdHash == validTestPasswdHash {
		t.Fatal("passwd_hash was not reset")
	}
	if err := users.VerifyPassword([]byte("new-password"), stored.PasswdHash); err != nil {
		t.Fatal(err)
	}
}

func TestRunUsersCreateRejectsPasswordStdinWithFileStdin(t *testing.T) {
	root := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(append(testGlobalArgs(root), "users", "create", "--file", "-", "--password-stdin"), &stdout, &stderr, strings.NewReader(`{"username":"alice","name":"Alice","admin":true}`), emptyEnv)
	if code == 0 {
		t.Fatalf("expected failure stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "cannot use --file - with --password-stdin") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

const validTestPasswdHash = "$argon2id$v=19$m=19456,t=2,p=1$WGeElL1uhb1hV8gVYptHmg$rvzcxwJQV6VK9gqH/GnHKWGB3t+40DX8XUYLkr8iiGE"

func testGlobalArgs(root string) []string {
	return []string{
		"--no-config",
		"--user-dir", filepath.Join(root, "users"),
		"--agent-dir", filepath.Join(root, "agents"),
		"--target-dir", filepath.Join(root, "targets"),
		"--policy-dir", filepath.Join(root, "policies"),
	}
}

func emptyEnv(string) (string, bool) {
	return "", false
}

func readJSONFile(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}
