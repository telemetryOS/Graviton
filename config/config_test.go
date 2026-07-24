package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mongo(name, dbName string) *DatabaseConfig {
	return &DatabaseConfig{
		Name:          name,
		Kind:          DatabaseKindMongoDB,
		ConnectionUrl: "mongodb://localhost:27017",
		DatabaseName:  dbName,
	}
}

func Test_Validate_Valid(t *testing.T) {
	conf := &Config{
		MigrationsDb:   "main",
		MigrationsPath: "./migrations",
		Databases:      []*DatabaseConfig{mongo("main", "neo_main"), mongo("legacy", "telemetry_v1")},
	}
	if err := conf.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func Test_Validate_NoDatabases(t *testing.T) {
	conf := &Config{MigrationsDb: "main"}
	err := conf.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want error for no databases")
	}
	if !strings.Contains(err.Error(), "no databases") {
		t.Errorf("error = %q, want mention of missing databases", err.Error())
	}
}

func Test_Validate_MissingMigrationsDb_MultipleDatabases(t *testing.T) {
	// With more than one database configured, migrations_db is required.
	conf := &Config{
		Databases: []*DatabaseConfig{mongo("main", "neo_main"), mongo("legacy", "telemetry_v1")},
	}
	err := conf.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want error for unset migrations_db")
	}
	if !strings.Contains(err.Error(), "migrations_db is not set") {
		t.Errorf("error = %q, want mention that migrations_db is not set", err.Error())
	}
	if !strings.Contains(err.Error(), "main") || !strings.Contains(err.Error(), "legacy") {
		t.Errorf("error = %q, want the configured databases listed", err.Error())
	}
}

func Test_Validate_UnknownMigrationsDb(t *testing.T) {
	conf := &Config{
		MigrationsDb: "bogus",
		Databases:    []*DatabaseConfig{mongo("main", "neo_main"), mongo("legacy", "telemetry_v1")},
	}
	err := conf.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want error for unknown migrations_db")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error = %q, want the bad migrations_db named", err.Error())
	}
	if !strings.Contains(err.Error(), "main") || !strings.Contains(err.Error(), "legacy") {
		t.Errorf("error = %q, want the configured databases listed", err.Error())
	}
}

func Test_Load_DefaultsMigrationsPath(t *testing.T) {
	dir := t.TempDir()
	const cfg = `migrations_db = "main"

[[databases]]
name = "main"
kind = "mongodb"
connection_url = "mongodb://localhost:27017"
database_name = "neo_main"
`
	if err := os.WriteFile(filepath.Join(dir, CONFIG_NAME), []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	withWorkingDir(t, dir)

	conf, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if conf == nil {
		t.Fatal("Load() = nil, want config")
	}
	if conf.MigrationsPath != DefaultMigrationsPath {
		t.Errorf("MigrationsPath = %q, want default %q", conf.MigrationsPath, DefaultMigrationsPath)
	}
	if conf.MigrationsDb != "main" {
		t.Errorf("MigrationsDb = %q, want main", conf.MigrationsDb)
	}
}

func Test_Load_DefaultsMigrationsDbToSingleDatabase(t *testing.T) {
	dir := t.TempDir()
	const cfg = `migrations_path = "./migrations"

[[databases]]
name = "only"
kind = "mongodb"
connection_url = "mongodb://localhost:27017"
database_name = "neo_only"
`
	if err := os.WriteFile(filepath.Join(dir, CONFIG_NAME), []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	withWorkingDir(t, dir)

	conf, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if conf.MigrationsDb != "only" {
		t.Errorf("MigrationsDb = %q, want it to default to the only database %q", conf.MigrationsDb, "only")
	}
	if err := conf.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil after single-database default", err)
	}
}

func Test_Load_EnvVarSubstitution(t *testing.T) {
	dir := t.TempDir()
	const cfg = `migrations_db = "main"
migrations_path = "./migrations"

[[databases]]
name = "main"
kind = "mongodb"
connection_url = "${GRAVITON_TEST_URL}"
database_name = "neo_main"
`
	if err := os.WriteFile(filepath.Join(dir, CONFIG_NAME), []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("GRAVITON_TEST_URL", "mongodb://example:27017")
	withWorkingDir(t, dir)

	conf, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := conf.Database("main").ConnectionUrl; got != "mongodb://example:27017" {
		t.Errorf("ConnectionUrl = %q, want substituted value", got)
	}
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
}
