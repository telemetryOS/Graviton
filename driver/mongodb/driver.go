package mongodb

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/telemetryos/graviton/config"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"

	"github.com/dop251/goja"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const MIGRATIONS_COLLECTION = "graviton-migrations"

type Options struct {
	URI      string
	Database string
}

type driverRuntimeData struct {
	objectIdCtorVal goja.Value
}

type Driver struct {
	config      *config.DatabaseConfig
	client      *mongo.Client
	database    *mongo.Database
	runtimeData map[*goja.Runtime]*driverRuntimeData

	// session is created lazily on first transactional use and reused for the
	// whole run (one session per database per run). sessionCtx and inTx track
	// the single transaction that may be open on that session at any time.
	session    mongo.Session
	sessionCtx mongo.SessionContext
	inTx       bool
}

// New builds a MongoDB driver for conf.
func New(conf *config.DatabaseConfig) *Driver {
	return &Driver{
		config:      conf,
		runtimeData: make(map[*goja.Runtime]*driverRuntimeData),
	}
}

func (d *Driver) Connect(ctx context.Context) error {
	clientOptions := options.Client().
		ApplyURI(d.config.ConnectionUrl)

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return err
	}

	d.client = client
	d.database = client.Database(d.config.DatabaseName)

	var buildInfo struct {
		Version string `bson:"version"`
	}
	result := d.database.RunCommand(ctx, bson.D{{Key: "buildInfo", Value: 1}})
	if err := result.Decode(&buildInfo); err != nil {
		return fmt.Errorf("failed to get MongoDB server version: %w", err)
	}
	if !isVersionAtLeast(buildInfo.Version, 4, 0) {
		return errors.New("MongoDB version must be at least 4.0")
	}

	var helloDB struct {
		IsWritablePrimary bool `bson:"isWritablePrimary"`
		IsWritable        bool `bson:"isWritable"`
		Secondary         bool `bson:"secondary"`
		HasReplica        bool `bson:"hasReplica"`
	}
	result = d.database.RunCommand(ctx, bson.D{{Key: "hello", Value: 1}})
	if err := result.Decode(&helloDB); err != nil {
		return fmt.Errorf("failed to get MongoDB server metadata: %w", err)
	}
	if !helloDB.IsWritable && !helloDB.IsWritablePrimary {
		return errors.New("MongoDB server is not writable")
	}
	if helloDB.Secondary {
		return errors.New("Graviton cannot write to a secondary MongoDB server")
	}
	if !helloDB.HasReplica && !helloDB.IsWritablePrimary {
		return errors.New("MongoDB server must be part of a replica set as transactions are required for Graviton")
	}

	return nil
}

func (d *Driver) Disconnect(ctx context.Context) error {
	if d.client == nil {
		return nil
	}
	if d.session != nil {
		d.session.EndSession(ctx)
		d.session = nil
	}
	return d.client.Disconnect(ctx)
}

func (d *Driver) Handle(ctx context.Context) any {
	return &MongoHandle{ctx: ctx, driver: d}
}

func (d *Driver) Init(ctx context.Context, runtime *goja.Runtime) {
	d.runtimeData[runtime] = &driverRuntimeData{
		objectIdCtorVal: runtime.ToValue(JSObjectIdCtor),
	}
}

func (d *Driver) Globals(ctx context.Context, runtime *goja.Runtime) map[string]any {
	globals := map[string]any{}
	globals["ObjectId"] = d.runtimeData[runtime].objectIdCtorVal
	return globals
}

// MaybeIntoJSValue surfaces primitive.ObjectID values to migration scripts as
// instances of the ObjectId JS class (so toHexString()/toString() work and
// round-tripping through filters converts back via MaybeFromJSValue).
func (d *Driver) MaybeIntoJSValue(ctx context.Context, jsvm *goja.Runtime, value any) (goja.Value, bool) {
	rtData := d.runtimeData[jsvm]
	if rtData == nil {
		return nil, false
	}
	// BSON binary payloads (e.g. credential key material) pass through as
	// opaque host values: scripts can carry and re-store them, and fromJs's
	// Export() returns the original primitive.Binary for BSON marshaling.
	if bin, isBin := value.(primitive.Binary); isBin {
		return jsvm.ToValue(bin), true
	}

	oid, ok := value.(primitive.ObjectID)
	if !ok {
		if p, isPtr := value.(*primitive.ObjectID); isPtr && p != nil {
			oid = *p
		} else {
			return nil, false
		}
	}
	inst, err := jsvm.New(rtData.objectIdCtorVal, jsvm.ToValue(oid.Hex()))
	if err != nil {
		panic(err)
	}
	return inst, true
}

func (d *Driver) MaybeFromJSValue(ctx context.Context, jsvm *goja.Runtime, val goja.Value) (any, bool) {
	rtData := d.runtimeData[jsvm]
	if rtData == nil {
		return nil, false
	}
	if IsObjectId(jsvm, val, rtData.objectIdCtorVal) {
		return ObjectIdFromJSValue(jsvm, val), true
	}
	// Host-wrapped BSON binary values (see MaybeIntoJSValue) export back to
	// their original primitive.Binary for BSON marshaling.
	if bin, ok := val.Export().(primitive.Binary); ok {
		return bin, true
	}
	return nil, false
}

func (d *Driver) GetAppliedMigrationsMetadata(ctx context.Context) ([]*migrationsmeta.MigrationMetadata, error) {
	findOptions := options.Find().SetSort(bson.D{
		{Key: "filename", Value: 1},
	})
	opCtx := d.opCtx(ctx)
	cur, err := d.getMigrationsCollection().Find(opCtx, bson.M{}, findOptions)
	if err != nil {
		return nil, err
	}
	defer cur.Close(opCtx)

	var migrationsMetadata []*migrationsmeta.MigrationMetadata
	if err := cur.All(opCtx, &migrationsMetadata); err != nil {
		return nil, err
	}

	return migrationsMetadata, nil
}

func (d *Driver) SetAppliedMigrationsMetadata(ctx context.Context, migrationsMetadata []*migrationsmeta.MigrationMetadata) error {
	opCtx := d.opCtx(ctx)
	migrationsCollection := d.getMigrationsCollection()
	_, err := migrationsCollection.DeleteMany(opCtx, bson.M{})
	if err != nil {
		return err
	}

	if len(migrationsMetadata) == 0 {
		return nil
	}

	var documents []any
	for _, migrationMetadata := range migrationsMetadata {
		documents = append(documents, migrationMetadata)
	}
	_, err = migrationsCollection.InsertMany(opCtx, documents)
	return err
}

// BeginTx opens a transaction on this driver's session if one is not already
// open, creating the session lazily on first use.
func (d *Driver) BeginTx(ctx context.Context) error {
	_, err := d.ensureTx(ctx)
	return err
}

// ensureTx returns the session context bound to this driver's open transaction,
// beginning one (and lazily creating the session) if none is open yet. It is
// the shared path behind both BeginTx and the JS-facing collection operations.
func (d *Driver) ensureTx(ctx context.Context) (mongo.SessionContext, error) {
	if d.inTx {
		return d.sessionCtx, nil
	}
	if d.session == nil {
		session, err := d.client.StartSession()
		if err != nil {
			return nil, err
		}
		d.session = session
	}
	if err := d.session.StartTransaction(); err != nil {
		return nil, err
	}
	d.sessionCtx = mongo.NewSessionContext(ctx, d.session)
	d.inTx = true
	return d.sessionCtx, nil
}

// CommitTx commits the open transaction, retrying the commit on the
// UnknownTransactionCommitResult label per the MongoDB transactions contract.
// The session is kept alive for subsequent transactions in the same run.
func (d *Driver) CommitTx(ctx context.Context) error {
	if !d.inTx {
		return nil
	}
	err := d.commitWithRetry(ctx)
	d.inTx = false
	d.sessionCtx = nil
	return err
}

func (d *Driver) commitWithRetry(ctx context.Context) error {
	for {
		err := d.session.CommitTransaction(ctx)
		if err == nil {
			return nil
		}
		var cmdErr mongo.CommandError
		if errors.As(err, &cmdErr) && cmdErr.HasErrorLabel("UnknownTransactionCommitResult") {
			continue
		}
		return err
	}
}

// RollbackTx aborts the open transaction, if any. The session is retained for
// subsequent transactions.
func (d *Driver) RollbackTx(ctx context.Context) error {
	if !d.inTx {
		return nil
	}
	err := d.session.AbortTransaction(ctx)
	d.inTx = false
	d.sessionCtx = nil
	return err
}

func (d *Driver) HasOpenTx() bool {
	return d.inTx
}

// RenameDatabase moves this driver's database to newName (a literal physical
// name) and drops the source. MongoDB has no native database rename, so it is
// implemented by renaming every non-system collection across databases with the
// admin renameCollection command and then dropping the emptied source.
//
// It is immediate and non-transactional by nature: it runs on the plain context
// and never opens a session/transaction. It refuses to run when a transaction is
// already open on this driver, because rename is not part of that transaction
// and would not roll back with it. renameCollection runs without dropTarget, so
// a pre-existing target collection surfaces as a loud error rather than silent
// data loss. Same cluster only — the one client renames within its own server.
func (d *Driver) RenameDatabase(ctx context.Context, newName string) error {
	if d.inTx {
		return fmt.Errorf("cannot rename database %q while a transaction is open on it", d.database.Name())
	}

	src := d.database.Name()
	if newName == src {
		return fmt.Errorf("cannot rename database %q to itself", src)
	}
	if newName == "" {
		return errors.New("cannot rename database to an empty name")
	}

	names, err := d.nonSystemCollectionNames(ctx)
	if err != nil {
		return err
	}

	admin := d.client.Database("admin")
	for _, coll := range names {
		cmd := bson.D{
			{Key: "renameCollection", Value: src + "." + coll},
			{Key: "to", Value: newName + "." + coll},
		}
		if err := admin.RunCommand(ctx, cmd).Err(); err != nil {
			return fmt.Errorf("failed to rename %s.%s to %s.%s: %w", src, coll, newName, coll, err)
		}
	}

	remaining, err := d.nonSystemCollectionNames(ctx)
	if err != nil {
		return err
	}
	if len(remaining) != 0 {
		return fmt.Errorf(
			"source database %q still has collections after rename, refusing to drop it: %s",
			src, strings.Join(remaining, ", "),
		)
	}

	if err := d.database.Drop(ctx); err != nil {
		return fmt.Errorf("failed to drop source database %q after rename: %w", src, err)
	}

	return nil
}

// nonSystemCollectionNames lists the source database's collections excluding the
// system.* namespace, which cannot (and must not) be renamed.
func (d *Driver) nonSystemCollectionNames(ctx context.Context) ([]string, error) {
	all, err := d.database.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(all))
	for _, name := range all {
		if strings.HasPrefix(name, "system.") {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}

// opCtx returns the context that binds an operation to this driver's open
// transaction, or the plain context when none is open. Unlike the JS-facing
// collection surface (which lazily begins a transaction), tracking reads and
// writes never start one implicitly — status reads run outside a transaction,
// and the marker write is wrapped in an explicit BeginTx by the runner.
func (d *Driver) opCtx(ctx context.Context) context.Context {
	if d.inTx {
		return d.sessionCtx
	}
	return ctx
}

func (d *Driver) getMigrationsCollection() *mongo.Collection {
	return d.database.Collection(MIGRATIONS_COLLECTION)
}

func isVersionAtLeast(version string, minMajor, minMinor int) bool {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	return major > minMajor || (major == minMajor && minor >= minMinor)
}
