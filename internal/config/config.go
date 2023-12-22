package config

import (
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

const CONFIG_NAME = "graviton.config.toml"

var testConfigPath string

func SetTestPath(configPath string) {
	testConfigPath = configPath
}

type ConfigMongoDB struct {
	URI            string `toml:"uri"`
	Database       string `toml:"database"`
	MigrationsPath string `toml:"migrations_path"`
}

type Config struct {
	ProjectPath string
	MongoDB     *ConfigMongoDB `toml:"mongodb"`
}

// GetFilePath returns the path to Graviton's config within the current project
// if one exists.
func GetFilePath() (string, error) {
	if testConfigPath != "" {
		return testConfigPath, nil
	}

	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	chunks := filepath.SplitList(dir)
	for i := len(chunks) - 1; i >= 0; i-- {
		targetPath := filepath.Join(filepath.Join(chunks[:i]...), CONFIG_NAME)
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

	var config Config
	if err := toml.NewDecoder(configFile).Decode(&config); err != nil {
		return nil, err
	}

	config.ProjectPath = filepath.Dir(configPath)

	return &config, nil
}
