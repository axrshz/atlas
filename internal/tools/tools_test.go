package tools

import (
	"testing"
)

func TestBashExecutesArbitraryCommand(t *testing.T) {
	result, err := Bash([]byte(`{"command":"printf unrestricted"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result != "unrestricted" {
		t.Fatalf("result = %q, want unrestricted", result)
	}
}
