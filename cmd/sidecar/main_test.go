package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDataFileCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vol.img")

	if err := ensureDataFile(path, 1024); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 1024 {
		t.Fatalf("size = %d", info.Size())
	}
}

func TestEnsureDataFilePreservesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vol.img")
	if err := os.WriteFile(path, []byte("existing-data"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := ensureDataFile(path, 1024); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "existing-data" {
		t.Fatalf("data = %q", string(data))
	}
}
