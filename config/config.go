package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const CONFIG_NAME = "graviton.config.toml"

// DefaultMigrationsPath is used when migrations_path is omitted from the config.
const DefaultMigrationsPath = "./migrations"

type DatabaseKind string

const (
	DatabaseKindMongoDB    DatabaseKind = "mongodb"
	DatabaseKindPostgreSQL DatabaseKind = "postgresql"
	DatabaseKindMySQL      DatabaseKind = "mysql"
	DatabaseKindSQLite     DatabaseKind = "sqlite"
)

type Config struct {
	ProjectPath string

	// MigrationsDb names the [[databases]] entry that holds the linear
	// applied-migrations tracking collection/table.
	MigrationsDb string `toml:"migrations_db"`

	// MigrationsPath is the single directory containing the one linear ordered
	// migration set for the whole project.
	MigrationsPath string `toml:"migrations_path"`

	Databases []*DatabaseConfig `toml:"databases"`
}

type DatabaseConfig struct {
	Name          string       `toml:"name"`
	Kind          DatabaseKind `toml:"kind"`
	ConnectionUrl string       `toml:"connection_url"`
	DatabaseName  string       `toml:"database_name"`
}

// GetFilePath returns the path to Graviton's config within the current project
// if one exists.
func GetFilePath() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	chunks := strings.Split(dir, string(filepath.Separator))
	for i := len(chunks); i != -1; i -= 1 {
		curPath := strings.Join(chunks[:i], string(filepath.Separator))
		targetPath := filepath.Join(curPath, CONFIG_NAME)
		if _, err := os.Stat(targetPath); err == nil {
			return targetPath, nil
		}
	}

	return "", nil
}

// Exists returns true if the config exists on disk
func Exists() bool {
	configPath, err := GetFilePath()
	if err != nil {
		return false
	}
	return configPath != ""
}

// Load loads the config from the current project if one exists.
func Load() (*Config, error) {
	configPath, err := GetFilePath()
	if err != nil {
		return nil, err
	}
	if configPath == "" {
		return nil, nil
	}

	configFile, err := os.OpenFile(configPath, os.O_RDONLY, 0644)
	if err != nil {
		return nil, err
	}
	defer configFile.Close()

	configSrc, err := io.ReadAll(configFile)
	if err != nil {
		return nil, err
	}

	// FIXME: THIS SUCKS, we need fallbacks
	// template in environment variables
	envVars := os.Environ()
	for _, envVar := range envVars {
		parts := strings.SplitN(envVar, "=", 2)
		if len(parts) != 2 {
			continue
		}
		configSrc = []byte(strings.ReplaceAll(string(configSrc), "${"+parts[0]+"}", parts[1]))
	}
	configSrcReader := strings.NewReader(string(configSrc))

	var config Config
	if err := toml.NewDecoder(configSrcReader).Decode(&config); err != nil {
		return nil, err
	}

	config.ProjectPath = filepath.Dir(configPath)

	if config.MigrationsPath == "" {
		config.MigrationsPath = DefaultMigrationsPath
	}

	// With exactly one database configured there is no ambiguity, so
	// migrations_db defaults to it and need not be set explicitly.
	if config.MigrationsDb == "" && len(config.Databases) == 1 {
		config.MigrationsDb = config.Databases[0].Name
	}

	return &config, nil
}

// Validate checks that the config is coherent for the single-linear-set model:
// at least one database must be configured, and migrations_db must name a
// configured [[databases]] entry.
func (c *Config) Validate() error {
	if len(c.Databases) == 0 {
		return fmt.Errorf("no databases are configured in %s", CONFIG_NAME)
	}
	if c.MigrationsDb == "" {
		return fmt.Errorf(
			"migrations_db is not set; it must name one of the configured databases: %s",
			strings.Join(c.DatabaseNames(), ", "),
		)
	}
	if c.Database(c.MigrationsDb) == nil {
		return fmt.Errorf(
			"migrations_db %q does not name a configured database; configured databases: %s",
			c.MigrationsDb, strings.Join(c.DatabaseNames(), ", "),
		)
	}
	return nil
}

// DatabaseNames returns the aliases of every configured database in config
// order.
func (c *Config) DatabaseNames() []string {
	names := make([]string, 0, len(c.Databases))
	for _, database := range c.Databases {
		names = append(names, database.Name)
	}
	return names
}

// MigrationsDatabase returns the configured entry that holds migration
// tracking.
func (c *Config) MigrationsDatabase() *DatabaseConfig {
	return c.Database(c.MigrationsDb)
}

func (c *Config) Database(name string) *DatabaseConfig {
	for _, database := range c.Databases {
		if database.Name == name {
			return database
		}
	}
	return nil
}
