package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReadJSON(t *testing.T) {
	fs := New(t.TempDir())
	input := map[string]string{"status": "created"}
	if err := fs.WriteJSON("task-1", input); err != nil {
		t.Fatal(err)
	}
	var output map[string]string
	if err := fs.ReadJSON("task-1", &output); err != nil {
		t.Fatal(err)
	}
	if output["status"] != "created" {
		t.Fatalf("status = %q", output["status"])
	}
}

func TestReadJSONReportsCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	fs := New(dir)
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output map[string]string
	if err := fs.ReadJSON("bad", &output); err == nil {
		t.Fatal("expected corrupt JSON error")
	}
}

func TestListJSONFiles(t *testing.T) {
	dir := t.TempDir()
	fs := New(dir)
	if err := fs.WriteJSON("a", map[string]string{"id": "a"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skip.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := fs.ListJSONFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("paths = %#v", paths)
	}
}
