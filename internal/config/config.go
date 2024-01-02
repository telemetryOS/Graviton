package config

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const CONFIG_NAME = "graviton.config.toml"

type DatabaseKind string

const (
	DatabaseKindMongoDB DatabaseKind = "mongodb"
)

type Config struct {
	ProjectPath string
	Databases   []*DatabaseConfig `toml:"databases"`
}

type DatabaseConfig struct {
	Name           string       `toml:"name"`
	Kind           DatabaseKind `toml:"kind"`
	ConnectionUrl  string       `toml:"connection_url"`
	DatabaseName   string       `toml:"database_name"`
	MigrationsPath string       `toml:"migrations_path"`
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

	return &config, nil
}

func (c *Config) Database(name string) *DatabaseConfig {
	for _, database := range c.Databases {
		if database.Name == name {
			return database
		}
	}
	return nil
}
