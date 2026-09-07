package users

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	writeUserFile(t, dir, "fcd2f3a9-d546-44f4-837b-0c7aa4219752.json", `{"id":"fcd2f3a9-d546-44f4-837b-0c7aa4219752","username":"demo-admin","name":"Demo Admin","admin":false,"status":"active","passwd_hash":"`+validTestPasswdHash+`"}`)
	writeUserFile(t, dir, "admin.json", `{"id":"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5","username":"admin","name":"Admin","admin":true,"status":"active","passwd_hash":"`+validTestPasswdHash+`"}`)

	registry, diagnostics, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if _, ok := registry.Find("fcd2f3a9-d546-44f4-837b-0c7aa4219752"); !ok {
		t.Fatal("expected user not loaded")
	}
	if _, ok := registry.FindByUsername("DEMO-ADMIN"); !ok {
		t.Fatal("expected user not loaded by normalized username")
	}
	user, ok := registry.Find("a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5")
	if !ok || user.Name != "Admin" || !user.Admin {
		t.Fatalf("admin = %#v, found=%t", user, ok)
	}
}

func TestLoadDirRejectsInvalidUsers(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "invalid json", body: `{`},
		{name: "missing id", body: `{"username":"demo-admin","name":"Demo Admin","admin":false,"status":"active","passwd_hash":"` + validTestPasswdHash + `"}`},
		{name: "missing username", body: `{"id":"fcd2f3a9-d546-44f4-837b-0c7aa4219752","name":"Demo Admin","admin":false,"passwd_hash":"` + validTestPasswdHash + `"}`},
		{name: "invalid username", body: `{"id":"fcd2f3a9-d546-44f4-837b-0c7aa4219752","username":"Demo Admin","name":"Demo Admin","admin":false,"passwd_hash":"` + validTestPasswdHash + `"}`},
		{name: "missing name", body: `{"id":"fcd2f3a9-d546-44f4-837b-0c7aa4219752","username":"demo-admin","admin":false,"passwd_hash":"` + validTestPasswdHash + `"}`},
		{name: "missing admin", body: `{"id":"fcd2f3a9-d546-44f4-837b-0c7aa4219752","username":"demo-admin","name":"Demo Admin","passwd_hash":"` + validTestPasswdHash + `"}`},
		{name: "missing status", body: `{"id":"fcd2f3a9-d546-44f4-837b-0c7aa4219752","username":"demo-admin","name":"Demo Admin","admin":false,"passwd_hash":"` + validTestPasswdHash + `"}`},
		{name: "unsupported status", body: `{"id":"fcd2f3a9-d546-44f4-837b-0c7aa4219752","username":"demo-admin","name":"Demo Admin","admin":false,"status":"disabled"}`},
		{name: "reset pending with passwd_hash", body: `{"id":"fcd2f3a9-d546-44f4-837b-0c7aa4219752","username":"demo-admin","name":"Demo Admin","admin":false,"status":"password_reset_pending","passwd_hash":"` + validTestPasswdHash + `"}`},
		{name: "missing passwd_hash", body: `{"id":"fcd2f3a9-d546-44f4-837b-0c7aa4219752","username":"demo-admin","name":"Demo Admin","admin":false,"status":"active"}`},
		{name: "invalid passwd_hash", body: `{"id":"fcd2f3a9-d546-44f4-837b-0c7aa4219752","username":"demo-admin","name":"Demo Admin","admin":false,"status":"active","passwd_hash":"bcrypt"}`},
		{name: "invalid theme", body: `{"id":"fcd2f3a9-d546-44f4-837b-0c7aa4219752","username":"demo-admin","name":"Demo Admin","admin":false,"status":"active","passwd_hash":"` + validTestPasswdHash + `","theme":"system"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeUserFile(t, dir, "user.json", tc.body)
			registry, diagnostics, err := LoadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(registry.All()) != 0 {
				t.Fatalf("registry = %#v", registry.All())
			}
			if len(diagnostics) != 1 {
				t.Fatalf("diagnostics = %#v", diagnostics)
			}
		})
	}
}

func TestLoadDirRejectsDuplicateID(t *testing.T) {
	dir := t.TempDir()
	writeUserFile(t, dir, "one.json", `{"id":"fcd2f3a9-d546-44f4-837b-0c7aa4219752","username":"demo-admin","name":"Demo Admin","admin":false,"status":"active","passwd_hash":"`+validTestPasswdHash+`"}`)
	writeUserFile(t, dir, "two.json", `{"id":"fcd2f3a9-d546-44f4-837b-0c7aa4219752","username":"duplicate","name":"Duplicate","admin":false,"status":"active","passwd_hash":"`+validTestPasswdHash+`"}`)

	registry, diagnostics, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.All()) != 1 {
		t.Fatalf("registry = %#v", registry.All())
	}
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestLoadDirRejectsDuplicateUsername(t *testing.T) {
	dir := t.TempDir()
	writeUserFile(t, dir, "one.json", `{"id":"fcd2f3a9-d546-44f4-837b-0c7aa4219752","username":"demo-admin","name":"Demo Admin","admin":false,"status":"active","passwd_hash":"`+validTestPasswdHash+`"}`)
	writeUserFile(t, dir, "two.json", `{"id":"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5","username":"demo-admin","name":"Duplicate","admin":false,"status":"active","passwd_hash":"`+validTestPasswdHash+`"}`)

	registry, diagnostics, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.All()) != 1 {
		t.Fatalf("registry = %#v", registry.All())
	}
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "duplicate username") {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestRootUserSchemaCoversRuntimeFields(t *testing.T) {
	root := repoRoot(t)
	schemaText, err := os.ReadFile(filepath.Join(root, "schemas", "proxyma.user.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(schemaText) {
		t.Fatal("proxyma.user.schema.json is not valid JSON")
	}
	schema := string(schemaText)
	for _, required := range []string{
		"$schema",
		"id",
		"username",
		"name",
		"admin",
		"status",
		"passwd_hash",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("proxyma.user.schema.json missing %q", required)
		}
	}
}

const validTestPasswdHash = "$argon2id$v=19$m=19456,t=2,p=1$WGeElL1uhb1hV8gVYptHmg$rvzcxwJQV6VK9gqH/GnHKWGB3t+40DX8XUYLkr8iiGE"

func writeUserFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root")
		}
		dir = parent
	}
}
