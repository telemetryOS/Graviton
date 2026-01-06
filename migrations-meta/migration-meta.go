package migrationsmeta

import (
	"regexp"
	"time"
)

// 000000000000-test.migration.ts
var MigrationNamePattern = regexp.MustCompile(`^\d{14}-([a-zA-Z-_]+)\.migration\.ts$`)

type MigrationMetadata struct {
	Filename  string    `bson:"filename"`
	Source    string    `bson:"source"`
	AppliedAt time.Time `bson:"applied_at"`
}

func (m *MigrationMetadata) Name() string {
	matches := MigrationNamePattern.FindStringSubmatch(m.Filename)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}
