package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/telemetryos/graviton/migrations"
)

// findMigrationIndex locates a migration by name within an ordered list,
// exiting with a message that lists the available names when it is not there.
//
// Every command taking a <migration> argument routes through this, because the
// naive form of the search is quietly dangerous. Migration.Name() extracts the
// descriptive part of the filename and returns "" for a filename that does not
// match the expected pattern, so comparing user input directly against it has
// two failure modes: a name that matches nothing leaves the caller to decide
// what "nothing" means, and an empty argument matches any malformed filename.
//
// set-head got the first one wrong — an unmatched name fell through its loop
// without breaking, so it marked every migration applied instead of stopping at
// one. On a production database that silently claims the whole set has run.
func findMigrationIndex(list []*migrations.Migration, name string, command string) int {
	if strings.TrimSpace(name) == "" {
		fmt.Fprintf(os.Stderr, "%s needs a migration name\n", command)
		os.Exit(1)
	}

	for i, migration := range list {
		// Guard against Name() returning "" for a malformed filename matching a
		// caller's empty-ish input; TrimSpace above makes that unreachable, and
		// this keeps it that way if the check above ever moves.
		if migration.Name() != "" && migration.Name() == name {
			return i
		}
	}

	available := []string{}
	for _, migration := range list {
		if migration.Name() != "" {
			available = append(available, migration.Name())
		}
	}

	fmt.Fprintf(os.Stderr, "no migration named %q\n", name)
	if len(available) == 0 {
		fmt.Fprintf(os.Stderr, "there are no migrations to choose from\n")
	} else {
		fmt.Fprintf(os.Stderr, "available: %s\n", strings.Join(available, ", "))
		fmt.Fprintf(os.Stderr, "use the name only, without the timestamp or .migration.ts suffix\n")
	}
	os.Exit(1)
	return -1
}
