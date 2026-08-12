package mongodb

import (
	"context"
	"errors"

	"github.com/telemetryos/graviton/driver/internal/lockretry"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// MIGRATIONS_LOCK_COLLECTION holds the whole-run migrations lock as a single
// document, a sibling of the tracking collection when this driver is the
// migrations_db. It is a separate collection so the tracking reads never have
// to filter lock documents out.
const MIGRATIONS_LOCK_COLLECTION = "graviton-migrations-lock"

// lockDocumentID is the fixed _id of the one lock document; inserting it is
// atomic, so a second insert fails with a duplicate-key error while the lock
// is held.
const lockDocumentID = "lock"

type lockDocument struct {
	ID                            string `bson:"_id"`
	migrationsmeta.MigrationsLock `bson:",inline"`
}

// AcquireMigrationsLock claims the lock by inserting the fixed-_id lock
// document. Lock operations always run on the plain context — they are
// immediate and never join this driver's open transaction.
func (d *Driver) AcquireMigrationsLock(ctx context.Context, lock *migrationsmeta.MigrationsLock) (*migrationsmeta.MigrationsLock, error) {
	return lockretry.Acquire(
		func() (bool, error) {
			_, err := d.getMigrationsLockCollection().InsertOne(ctx, &lockDocument{
				ID:             lockDocumentID,
				MigrationsLock: *lock,
			})
			if mongo.IsDuplicateKeyError(err) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			return true, nil
		},
		func() (*migrationsmeta.MigrationsLock, error) {
			return d.GetMigrationsLock(ctx)
		},
	)
}

// ReleaseMigrationsLock deletes the lock document only while holder still owns
// it; the filtered DeleteOne is atomic.
func (d *Driver) ReleaseMigrationsLock(ctx context.Context, holder string) error {
	_, err := d.getMigrationsLockCollection().DeleteOne(ctx, bson.M{
		"_id":    lockDocumentID,
		"holder": holder,
	})
	return err
}

func (d *Driver) GetMigrationsLock(ctx context.Context) (*migrationsmeta.MigrationsLock, error) {
	var doc lockDocument
	err := d.getMigrationsLockCollection().FindOne(ctx, bson.M{"_id": lockDocumentID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &doc.MigrationsLock, nil
}

func (d *Driver) ClearMigrationsLock(ctx context.Context) error {
	_, err := d.getMigrationsLockCollection().DeleteMany(ctx, bson.M{})
	return err
}

func (d *Driver) getMigrationsLockCollection() *mongo.Collection {
	return d.database.Collection(MIGRATIONS_LOCK_COLLECTION)
}
