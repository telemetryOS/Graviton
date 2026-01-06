package migrationsmeta

import (
	"testing"
	"time"
)

func Test_MigrationMetadata_Name(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"20231225010950-one.migration.ts", "one"},
		{"20231225010956-two.migration.ts", "two"},
		{"20231225011003-three.migration.ts", "three"},
		{"12345678901234-test-migration.migration.ts", "test-migration"},
		{"00000000000000-test_underscore.migration.ts", "test_underscore"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			m := &MigrationMetadata{
				Filename:  tt.filename,
				Source:    "test source",
				AppliedAt: time.Now(),
			}

			result := m.Name()
			if result != tt.expected {
				t.Errorf("Name() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func Test_MigrationMetadata_Name_InvalidFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
	}{
		{"missing timestamp", "one.migration.ts"},
		{"short timestamp", "123-one.migration.ts"},
		{"wrong extension", "20231225010950-one.migration.js"},
		{"no dash", "20231225010950one.migration.ts"},
		{"empty filename", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &MigrationMetadata{
				Filename:  tt.filename,
				Source:    "test source",
				AppliedAt: time.Now(),
			}

			defer func() {
				if r := recover(); r == nil {
					t.Errorf("Name() should panic for invalid filename %q, but did not", tt.filename)
				}
			}()

			m.Name()
		})
	}
}

func Test_MigrationNamePattern(t *testing.T) {
	tests := []struct {
		filename string
		matches  bool
	}{
		{"20231225010950-one.migration.ts", true},
		{"12345678901234-test-name.migration.ts", true},
		{"00000000000000-test_underscore.migration.ts", true},
		{"123-short.migration.ts", false},
		{"20231225010950-one.migration.js", false},
		{"one.migration.ts", false},
		{"20231225010950one.migration.ts", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			matches := MigrationNamePattern.MatchString(tt.filename)
			if matches != tt.matches {
				t.Errorf("Pattern match for %q = %v, want %v", tt.filename, matches, tt.matches)
			}
		})
	}
}
