package instances

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxyma/internal/interaction"
)

const testNtfyInstanceID = "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"
const testNtfyCreatorUserID = "fcd2f3a9-d546-44f4-837b-0c7aa4219752"

func TestLoadDirLoadsEnabledNtfyInstances(t *testing.T) {
	root := t.TempDir()
	writeNtfyInstance(t, root, "ops.json", `{
  "id": "`+testNtfyInstanceID+`",
  "enabled": true,
  "base_url": "https://ntfy.example/",
  "request_topic": "ops-requests",
  "response_topic": "ops-responses",
  "poll_interval": "2s",
  "poll_timeout": "3s",
  "responded_by": "ops-phone"
}`)

	registry, diagnostics, err := LoadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if !registry.HasEnabled(testNtfyInstanceID) {
		t.Fatal("ntfy instance was not loaded")
	}
	if !registry.HasEnabled("") || !registry.HasEnabled(interaction.DefaultInstanceLocal) {
		t.Fatal("local instance should always be available")
	}
	items := registry.Enabled()
	if len(items) != 1 {
		t.Fatalf("enabled instances = %#v", items)
	}
	instance, ok := items[0].(NtfyInstance)
	if !ok {
		t.Fatalf("definition type = %T", items[0])
	}
	if instance.BaseURL != "https://ntfy.example" || instance.PollInterval != 2*time.Second || instance.PollTimeout != 3*time.Second {
		t.Fatalf("unexpected instance = %#v", instance)
	}
	metadata := instance.Metadata()
	if metadata.ID != testNtfyInstanceID || metadata.ProviderType != ProviderNtfy || metadata.CreatorUserID != testNtfyCreatorUserID || !metadata.Enabled {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestLoadDirSkipsDisabledNtfyInstances(t *testing.T) {
	root := t.TempDir()
	writeNtfyInstance(t, root, "disabled.json", `{
  "id": "`+testNtfyInstanceID+`",
  "enabled": false,
  "base_url": "https://ntfy.example",
  "request_topic": "requests",
  "response_topic": "responses",
  "poll_interval": "2s",
  "poll_timeout": "3s"
}`)

	registry, diagnostics, err := LoadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if registry.HasEnabled(testNtfyInstanceID) {
		t.Fatal("disabled instance should not be enabled")
	}
	if enabled := registry.Enabled(); len(enabled) != 0 {
		t.Fatalf("enabled instances = %#v", enabled)
	}
	all := registry.All()
	if len(all) != 1 {
		t.Fatalf("all instances = %#v", all)
	}
	metadata := all[0].Metadata()
	if metadata.ID != testNtfyInstanceID || metadata.ProviderType != ProviderNtfy || metadata.Enabled {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestLoadDirRejectsDuplicateIDs(t *testing.T) {
	root := t.TempDir()
	body := `{
  "id": "` + testNtfyInstanceID + `",
  "enabled": true,
  "base_url": "https://ntfy.example",
  "request_topic": "requests",
  "response_topic": "responses",
  "poll_interval": "2s",
  "poll_timeout": "3s"
}`
	writeNtfyInstance(t, root, "one.json", body)
	writeNtfyInstance(t, root, "two.json", body)

	_, diagnostics, err := LoadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "duplicate interaction instance id") {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestLoadDirRejectsNamedNtfyInstanceID(t *testing.T) {
	root := t.TempDir()
	writeNtfyInstance(t, root, "named.json", `{
  "id": "ops-phone",
  "enabled": true,
  "base_url": "https://ntfy.example",
  "request_topic": "requests",
  "response_topic": "responses",
  "poll_interval": "2s",
  "poll_timeout": "3s"
}`)

	_, diagnostics, err := LoadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "id must be a UUID v4") {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestLoadDirRejectsReservedLocalNtfyInstanceID(t *testing.T) {
	root := t.TempDir()
	writeNtfyInstance(t, root, "local.json", `{
  "id": "local",
  "enabled": true,
  "base_url": "https://ntfy.example",
  "request_topic": "requests",
  "response_topic": "responses",
  "poll_interval": "2s",
  "poll_timeout": "3s"
}`)

	_, diagnostics, err := LoadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "reserved") {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestLoadDirRejectsUnsupportedProviderDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "email"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, diagnostics, err := LoadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "unsupported interaction provider directory") {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestLoadDirRejectsLocalDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "local"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, diagnostics, err := LoadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "built in") {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestLoadDirRejectsMalformedDurationAndMissingTopic(t *testing.T) {
	root := t.TempDir()
	writeNtfyInstance(t, root, "bad-duration.json", `{
  "id": "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8",
  "enabled": true,
  "base_url": "https://ntfy.example",
  "request_topic": "requests",
  "response_topic": "responses",
  "poll_interval": "soon",
  "poll_timeout": "3s"
}`)
	writeNtfyInstance(t, root, "missing-topic.json", `{
  "id": "7937dd2d-0b8a-4880-8afa-0ea60fd65a43",
  "enabled": true,
  "base_url": "https://ntfy.example",
  "request_topic": "",
  "response_topic": "responses",
  "poll_interval": "2s",
  "poll_timeout": "3s"
}`)

	_, diagnostics, err := LoadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestLoadDirRejectsMissingOrMalformedCreatorUserID(t *testing.T) {
	root := t.TempDir()
	writeRawNtfyInstance(t, root, "missing-owner.json", `{
  "id": "`+testNtfyInstanceID+`",
  "enabled": true,
  "base_url": "https://ntfy.example",
  "request_topic": "requests",
  "response_topic": "responses",
  "poll_interval": "2s",
  "poll_timeout": "3s"
}`)
	writeRawNtfyInstance(t, root, "bad-owner.json", `{
  "id": "7937dd2d-0b8a-4880-8afa-0ea60fd65a43",
  "creator_user_id": "antoine",
  "enabled": true,
  "base_url": "https://ntfy.example",
  "request_topic": "requests",
  "response_topic": "responses",
  "poll_interval": "2s",
  "poll_timeout": "3s"
}`)

	_, diagnostics, err := LoadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestLoadDirMissingDirectoryDefaultsToLocalOnly(t *testing.T) {
	registry, diagnostics, err := LoadDir(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if !registry.HasEnabled("local") || registry.HasEnabled("missing") {
		t.Fatalf("unexpected registry: %#v", registry)
	}
}

func writeNtfyInstance(t *testing.T, root string, name string, body string) {
	t.Helper()
	if !strings.Contains(body, `"creator_user_id"`) {
		body = strings.Replace(body, `"enabled":`, `"creator_user_id": "`+testNtfyCreatorUserID+`",
  "enabled":`, 1)
	}
	writeRawNtfyInstance(t, root, name, body)
}

func writeRawNtfyInstance(t *testing.T, root string, name string, body string) {
	t.Helper()
	dir := filepath.Join(root, "ntfy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
