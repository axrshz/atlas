package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvironmentLoadsSharedFile(t *testing.T) {
	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".atlas")
	if err := os.Mkdir(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, ".env"), []byte("ATLAS_TEST_SHARED=value\n"), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("USERPROFILE", homeDir)
	t.Setenv("HOME", homeDir)
	oldValue, wasSet := os.LookupEnv("ATLAS_TEST_SHARED")
	if err := os.Unsetenv("ATLAS_TEST_SHARED"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if wasSet {
			_ = os.Setenv("ATLAS_TEST_SHARED", oldValue)
		} else {
			_ = os.Unsetenv("ATLAS_TEST_SHARED")
		}
	})
	if err := loadEnvironment(); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("ATLAS_TEST_SHARED"); got != "value" {
		t.Fatalf("ATLAS_TEST_SHARED = %q, want value", got)
	}
}
