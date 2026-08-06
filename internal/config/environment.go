package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// LoadEnvironment loads workspace configuration before the shared user-level
// fallback while preserving variables already set in the process.
func LoadEnvironment() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find user home directory: %w", err)
	}

	for _, envFile := range []string{".env", filepath.Join(homeDir, ".atlas", ".env")} {
		if err := godotenv.Load(envFile); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("load %s: %w", envFile, err)
		}
	}
	return nil
}
