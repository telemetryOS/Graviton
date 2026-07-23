package commands_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const unknownDatabaseConfig = `[[databases]]
name = "main"
kind = "mongodb"
connection_url = "mongodb://localhost:27017"
database_name = "neo_main"

[[databases]]
name = "legacy"
kind = "mongodb"
connection_url = "mongodb://localhost:27017"
database_name = "telemetry_v1"
`

// Test_Status_UnknownDatabase verifies that an unknown database name produces a
// clean error listing the configured databases instead of a nil-dereference
// panic. It builds the binary and runs it against a temporary project so it
// exercises the real command path (the command errors before connecting to any
// database, so no MongoDB is required).
func Test_Status_UnknownDatabase(t *testing.T) {
	projectDir := t.TempDir()

	configPath := filepath.Join(projectDir, "graviton.config.toml")
	if err := os.WriteFile(configPath, []byte(unknownDatabaseConfig), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	binPath := filepath.Join(t.TempDir(), "graviton")
	build := exec.Command("go", "build", "-o", binPath, "github.com/telemetryos/graviton/cmd/graviton")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("failed to build graviton binary: %v\n%s", err, out)
	}

	cmd := exec.Command(binPath, "status", "bogus")
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	output := string(out)

	if err == nil {
		t.Fatalf("expected non-zero exit for unknown database, got success. output:\n%s", output)
	}
	if _, ok := err.(*exec.ExitError); !ok {
		t.Fatalf("expected exit error, got %v. output:\n%s", err, output)
	}

	if strings.Contains(output, "panic") {
		t.Errorf("output should not contain a panic:\n%s", output)
	}
	if !strings.Contains(output, "Unknown database `bogus`") {
		t.Errorf("output should name the unknown database, got:\n%s", output)
	}
	if !strings.Contains(output, "main") || !strings.Contains(output, "legacy") {
		t.Errorf("output should list configured database names, got:\n%s", output)
	}
}
