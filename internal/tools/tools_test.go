package tools

import (
	"strings"
	"testing"
)

func TestBashRejectsUnapprovedCommand(t *testing.T) {
	_, err := Bash([]byte(`{"command":"rm -rf ."}`))
	if err == nil {
		t.Fatal("expected unapproved command to be rejected")
	}
}

func TestSafeCommandEnvironmentRemovesSecrets(t *testing.T) {
	t.Setenv("POOLSIDE_API_KEY", "test-key")
	t.Setenv("TEST_VISIBLE_VALUE", "visible")

	environment := safeCommandEnvironment()
	for _, entry := range environment {
		if strings.HasPrefix(entry, "POOLSIDE_API_KEY=") {
			t.Fatal("poolside api key was passed to the command environment")
		}
	}

	if !containsEnvironmentEntry(environment, "TEST_VISIBLE_VALUE=visible") {
		t.Fatal("non-sensitive environment variable was removed")
	}
}

func TestLimitedBufferTruncatesOutput(t *testing.T) {
	var buffer limitedBuffer
	data := []byte(strings.Repeat("x", maxCommandOutputBytes+1))

	if _, err := buffer.Write(data); err != nil {
		t.Fatalf("write returned an error: %v", err)
	}
	if !buffer.truncated {
		t.Fatal("expected buffer to truncate output")
	}
	if buffer.Len() != maxCommandOutputBytes {
		t.Fatalf("buffer length = %d, want %d", buffer.Len(), maxCommandOutputBytes)
	}
}

func containsEnvironmentEntry(environment []string, expected string) bool {
	for _, entry := range environment {
		if entry == expected {
			return true
		}
	}
	return false
}
