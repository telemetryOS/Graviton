package commands_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildGraviton compiles the CLI once for a test and returns the binary path.
func buildGraviton(t *testing.T) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "graviton")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", binPath, "github.com/telemetryos/graviton/cmd/graviton")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("failed to build graviton binary: %v\n%s", err, out)
	}
	return binPath
}

// runStatus runs `graviton status` in a project configured with configTOML and
// returns the combined output and whether the command exited non-zero. The
// validation checks run before any database connection, so no database is
// required.
func runStatus(t *testing.T, configTOML string) (string, bool) {
	t.Helper()

	projectDir := t.TempDir()
	configPath := filepath.Join(projectDir, "graviton.config.toml")
	if err := os.WriteFile(configPath, []byte(configTOML), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cmd := exec.Command(buildGraviton(t), "status")
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	return string(out), err != nil
}

func Test_Status_MissingMigrationsDb(t *testing.T) {
	// With more than one database configured, migrations_db must be set. (With
	// exactly one database it defaults to that database, so no error there.)
	const cfg = `migrations_path = "./migrations"

[[databases]]
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
	output, failed := runStatus(t, cfg)

	if !failed {
		t.Fatalf("expected non-zero exit when migrations_db is unset with multiple databases. output:\n%s", output)
	}
	if strings.Contains(output, "panic") {
		t.Errorf("output should not contain a panic:\n%s", output)
	}
	if !strings.Contains(output, "migrations_db is not set") {
		t.Errorf("output should explain migrations_db is required, got:\n%s", output)
	}
	if !strings.Contains(output, "main") || !strings.Contains(output, "legacy") {
		t.Errorf("output should list the configured databases, got:\n%s", output)
	}
}

func Test_Status_UnknownMigrationsDb(t *testing.T) {
	const cfg = `migrations_db = "bogus"
migrations_path = "./migrations"

[[databases]]
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
	output, failed := runStatus(t, cfg)

	if !failed {
		t.Fatalf("expected non-zero exit when migrations_db names no configured database. output:\n%s", output)
	}
	if strings.Contains(output, "panic") {
		t.Errorf("output should not contain a panic:\n%s", output)
	}
	if !strings.Contains(output, "bogus") {
		t.Errorf("output should name the bad migrations_db, got:\n%s", output)
	}
	if !strings.Contains(output, "main") || !strings.Contains(output, "legacy") {
		t.Errorf("output should list configured database names, got:\n%s", output)
	}
}
