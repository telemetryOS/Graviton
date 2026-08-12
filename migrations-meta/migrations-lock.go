package migrationsmeta

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"
)

// MigrationsLock identifies the process holding the whole-run migrations lock
// in the tracking database. One lock guards the whole project, no matter how
// many databases are configured: it is claimed in migrations_db before any
// migration body runs and released when the run finishes. A lock left behind
// by a crashed run is cleared with `graviton unlock`.
type MigrationsLock struct {
	// Holder uniquely identifies the run that took the lock; release is
	// conditional on it so one run cannot release another's lock.
	Holder     string    `bson:"holder" json:"holder"`
	Hostname   string    `bson:"hostname" json:"hostname"`
	Pid        int       `bson:"pid" json:"pid"`
	AcquiredAt time.Time `bson:"acquired_at" json:"acquired_at"`
}

// NewMigrationsLock builds the lock record for the current process.
func NewMigrationsLock() *MigrationsLock {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	randomSuffix := make([]byte, 8)
	if _, err := rand.Read(randomSuffix); err != nil {
		panic(err)
	}

	pid := os.Getpid()
	return &MigrationsLock{
		Holder:     fmt.Sprintf("%s-%d-%s", hostname, pid, hex.EncodeToString(randomSuffix)),
		Hostname:   hostname,
		Pid:        pid,
		AcquiredAt: time.Now().UTC(),
	}
}
