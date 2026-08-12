package driver

import (
	"strings"
	"testing"

	"github.com/telemetryos/graviton/config"
)

func Test_ValidateDatabaseConfig(t *testing.T) {
	valid := []*config.DatabaseConfig{
		{Name: "assets", Kind: config.DatabaseKindS3, ConnectionUrl: "s3://bucket/prefix?region=us-east-1"},
		{Name: "cache", Kind: config.DatabaseKindRedis, ConnectionUrl: "redis://localhost:6379/0"},
		{Name: "main", Kind: config.DatabaseKindMongoDB, ConnectionUrl: "mongodb://localhost:27017"},
	}
	for _, conf := range valid {
		if err := ValidateDatabaseConfig(conf); err != nil {
			t.Errorf("ValidateDatabaseConfig(%s) error = %v, want nil", conf.Name, err)
		}
	}

	invalid := []*config.DatabaseConfig{
		{Name: "assets", Kind: config.DatabaseKindS3, ConnectionUrl: "http://not-s3/bucket"},
		{Name: "assets", Kind: config.DatabaseKindS3, ConnectionUrl: "s3://"},
		{Name: "cache", Kind: config.DatabaseKindRedis, ConnectionUrl: "localhost:6379"},
	}
	for _, conf := range invalid {
		err := ValidateDatabaseConfig(conf)
		if err == nil {
			t.Errorf("ValidateDatabaseConfig(%s %q) = nil, want error", conf.Kind, conf.ConnectionUrl)
			continue
		}
		if !strings.Contains(err.Error(), conf.Name) {
			t.Errorf("error %v should name the database", err)
		}
	}
}
