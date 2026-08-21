package commands_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fileStoreConfig = `migrations_path = "./selected-migrations"

[[databases]]
name = "files"
kind = "fs"
connection_url = "./store"
`

const gravitonCommandTimeout = 10 * time.Second

func runGravitonCommand(t *testing.T, bin, dir string, args ...string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), gravitonCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("graviton %s timed out after %s\n%s", strings.Join(args, " "), gravitonCommandTimeout, out)
	}
	return out, err
}

func Test_ConfigFlag_IsAvailableToEveryCommand(t *testing.T) {
	bin := buildGraviton(t)
	commands := [][]string{
		{"--help"},
		{"up", "--help"},
		{"down", "--help"},
		{"status", "--help"},
		{"set-head", "--help"},
		{"create", "--help"},
		{"unlock", "--help"},
		{"upgrade", "--help"},
	}
	for _, args := range commands {
		out, err := runGravitonCommand(t, bin, "", args...)
		if err != nil {
			t.Fatalf("graviton %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
		if !strings.Contains(string(out), "--config string") {
			t.Errorf("graviton %s help does not expose the global --config flag:\n%s", strings.Join(args, " "), out)
		}
	}
}

func Test_ConfigFlag_ExplicitPathWinsAndMigrationsPathIsConfigRelative(t *testing.T) {
	bin := buildGraviton(t)
	projectDir := t.TempDir()
	configDir := filepath.Join(projectDir, "configs")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("create config directory: %v", err)
	}

	discovered := strings.Replace(fileStoreConfig, "./selected-migrations", "./wrong-migrations", 1)
	if err := os.WriteFile(filepath.Join(projectDir, "graviton.config.toml"), []byte(discovered), 0644); err != nil {
		t.Fatalf("write discovered config: %v", err)
	}
	explicitPath := filepath.Join(configDir, "development.toml")
	if err := os.WriteFile(explicitPath, []byte(fileStoreConfig), 0644); err != nil {
		t.Fatalf("write explicit config: %v", err)
	}

	relativeConfigPath := filepath.Join("configs", "development.toml")
	for _, args := range [][]string{
		{"--config", relativeConfigPath, "create", "leading-flag"},
		{"create", "trailing-flag", "--config", relativeConfigPath},
	} {
		if out, err := runGravitonCommand(t, bin, projectDir, args...); err != nil {
			t.Fatalf("graviton %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	selectedDir := filepath.Join(configDir, "selected-migrations")
	for _, name := range []string{"leading-flag", "trailing-flag"} {
		matches, err := filepath.Glob(filepath.Join(selectedDir, "*-"+name+".migration.ts"))
		if err != nil {
			t.Fatalf("glob migration %q: %v", name, err)
		}
		if len(matches) != 1 {
			t.Errorf("migration %q matches = %v, want one file under explicit config directory", name, matches)
		}
	}
	if _, err := os.Stat(filepath.Join(projectDir, "wrong-migrations")); !os.IsNotExist(err) {
		t.Errorf("discovered config migrations directory should not be used; stat error = %v", err)
	}
}

func Test_ConfigFlag_UnreadableExplicitPathFailsBeforeDiscoveredConfig(t *testing.T) {
	bin := buildGraviton(t)
	projectDir := t.TempDir()

	discovered := strings.Replace(fileStoreConfig, `kind = "fs"`, `kind = "unsupported"`, 1)
	if err := os.WriteFile(filepath.Join(projectDir, "graviton.config.toml"), []byte(discovered), 0644); err != nil {
		t.Fatalf("write discovered config: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(projectDir, "unreadable-config"), 0755); err != nil {
		t.Fatalf("create unreadable config path: %v", err)
	}

	for _, path := range []string{
		filepath.Join("configs", "missing.toml"),
		"unreadable-config",
	} {
		out, err := runGravitonCommand(t, bin, projectDir, "status", "--config", path)
		if err == nil {
			t.Fatalf("graviton status with unreadable explicit config %q succeeded:\n%s", path, out)
		}
		output := string(out)
		if !strings.Contains(output, "read config") || !strings.Contains(output, path) {
			t.Errorf("output should name unreadable explicit config %q, got:\n%s", path, output)
		}
		if strings.Contains(output, "unknown kind") || strings.Contains(output, "connection") {
			t.Errorf("command used discovered config or attempted a connection before failing for %q:\n%s", path, output)
		}
	}
}
