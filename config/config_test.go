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

func Test_LoadPath_ExplicitRelativePathWinsAndAnchorsProject(t *testing.T) {
	projectDir := t.TempDir()
	explicitDir := filepath.Join(projectDir, "configs")
	if err := os.MkdirAll(explicitDir, 0755); err != nil {
		t.Fatalf("create config directory: %v", err)
	}

	const discovered = `[[databases]]
name = "discovered"
kind = "mongodb"
connection_url = "mongodb://localhost:27017"
database_name = "discovered"
`
	if err := os.WriteFile(filepath.Join(projectDir, CONFIG_NAME), []byte(discovered), 0644); err != nil {
		t.Fatalf("write discovered config: %v", err)
	}

	const explicit = `migrations_path = "./selected-migrations"

[[databases]]
name = "selected"
kind = "mongodb"
connection_url = "mongodb://localhost:27017"
database_name = "selected"
`
	explicitPath := filepath.Join(explicitDir, "development.toml")
	if err := os.WriteFile(explicitPath, []byte(explicit), 0644); err != nil {
		t.Fatalf("write explicit config: %v", err)
	}

	withWorkingDir(t, projectDir)
	conf, err := LoadPath(filepath.Join("configs", "development.toml"))
	if err != nil {
		t.Fatalf("LoadPath() error = %v", err)
	}
	if conf.MigrationsDb != "selected" {
		t.Errorf("MigrationsDb = %q, want selected explicit config", conf.MigrationsDb)
	}
	if conf.ProjectPath != explicitDir {
		t.Errorf("ProjectPath = %q, want explicit config directory %q", conf.ProjectPath, explicitDir)
	}
	wantMigrationsPath := filepath.Join(explicitDir, "selected-migrations")
	if got := filepath.Join(conf.ProjectPath, conf.MigrationsPath); got != wantMigrationsPath {
		t.Errorf("resolved migrations path = %q, want %q", got, wantMigrationsPath)
	}
}

func Test_LoadPath_MissingExplicitConfigReturnsReadError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.toml")
	conf, err := LoadPath(missing)
	if err == nil {
		t.Fatalf("LoadPath(%q) = %#v, nil; want read error", missing, conf)
	}
	if !strings.Contains(err.Error(), "read config") || !strings.Contains(err.Error(), missing) {
		t.Errorf("error = %q, want read error naming explicit path", err.Error())
	}
}

func Test_LoadPath_UnreadableExplicitConfigReturnsReadError(t *testing.T) {
	unreadable := t.TempDir()
	conf, err := LoadPath(unreadable)
	if err == nil {
		t.Fatalf("LoadPath(%q) = %#v, nil; want read error", unreadable, conf)
	}
	if !strings.Contains(err.Error(), "read config") || !strings.Contains(err.Error(), unreadable) {
		t.Errorf("error = %q, want read error naming explicit path", err.Error())
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

func Test_Validate_DuplicateNames(t *testing.T) {
	conf := &Config{
		MigrationsDb: "main",
		Databases:    []*DatabaseConfig{mongo("main", "a"), mongo("main", "b")},
	}
	err := conf.Validate()
	if err == nil || !strings.Contains(err.Error(), "unique") {
		t.Errorf("Validate() = %v, want duplicate-name error", err)
	}
}

func Test_Validate_UnnamedDatabase(t *testing.T) {
	conf := &Config{
		MigrationsDb: "main",
		Databases:    []*DatabaseConfig{mongo("main", "a"), mongo("", "b")},
	}
	err := conf.Validate()
	if err == nil || !strings.Contains(err.Error(), "no name") {
		t.Errorf("Validate() = %v, want unnamed-entry error", err)
	}
}

func Test_Validate_UnknownKind(t *testing.T) {
	conf := &Config{
		MigrationsDb: "main",
		Databases: []*DatabaseConfig{
			{Name: "main", Kind: "oracle", ConnectionUrl: "oracle://x"},
		},
	}
	err := conf.Validate()
	if err == nil || !strings.Contains(err.Error(), "unknown kind") || !strings.Contains(err.Error(), "redis") {
		t.Errorf("Validate() = %v, want unknown-kind error listing supported kinds", err)
	}
}

func Test_Validate_MissingConnectionUrl(t *testing.T) {
	conf := &Config{
		MigrationsDb: "main",
		Databases: []*DatabaseConfig{
			{Name: "main", Kind: DatabaseKindPostgreSQL},
		},
	}
	err := conf.Validate()
	if err == nil || !strings.Contains(err.Error(), "connection_url") {
		t.Errorf("Validate() = %v, want missing-connection_url error", err)
	}
}

func Test_Validate_StoreKindWithDatabaseName(t *testing.T) {
	conf := &Config{
		MigrationsDb: "files",
		Databases: []*DatabaseConfig{
			{Name: "files", Kind: DatabaseKindFS, ConnectionUrl: "./store", DatabaseName: "oops"},
		},
	}
	err := conf.Validate()
	if err == nil || !strings.Contains(err.Error(), "database_name") {
		t.Errorf("Validate() = %v, want database_name-unused error", err)
	}
}
