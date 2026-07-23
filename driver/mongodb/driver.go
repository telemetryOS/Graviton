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
	databases   []*config.DatabaseConfig
	client      *mongo.Client
	database    *mongo.Database
	sessionCtx  mongo.SessionContext
	runtimeData map[*goja.Runtime]*driverRuntimeData
}

// New builds a MongoDB driver for conf. databases is the full set of configured
// [[databases]] entries, retained so the migration handle's sibling(alias)
// accessor can resolve a stable database alias to its per-environment physical
// database_name. It may be nil when sibling resolution is not needed.
func New(conf *config.DatabaseConfig, databases []*config.DatabaseConfig) *Driver {
	return &Driver{
		config:      conf,
		databases:   databases,
		runtimeData: make(map[*goja.Runtime]*driverRuntimeData),
	}
}

// resolveSiblingDatabaseName resolves a configured database alias (the `name`
// field of a [[databases]] entry) to the physical database_name that db(name)
// expects. Sibling access reuses this driver's client, session, and
// transaction, so the alias must name another mongodb database configured on
// the same connection/cluster; otherwise a clean error is returned.
func (d *Driver) resolveSiblingDatabaseName(alias string) (string, error) {
	var conf *config.DatabaseConfig
	for _, database := range d.databases {
		if database.Name == alias {
			conf = database
			break
		}
	}
	if conf == nil {
		return "", fmt.Errorf(
			"no database aliased %q is configured; configured databases: %s",
			alias, strings.Join(d.configuredDatabaseAliases(), ", "),
		)
	}
	if conf.Kind != config.DatabaseKindMongoDB {
		return "", fmt.Errorf(
			"database %q is a %s database; sibling() only reaches mongodb databases on the same cluster",
			alias, conf.Kind,
		)
	}
	if conf.ConnectionUrl != d.config.ConnectionUrl {
		return "", fmt.Errorf(
			"database %q is on a different MongoDB connection; sibling() only reaches databases on the same cluster",
			alias,
		)
	}
	return conf.DatabaseName, nil
}

func (d *Driver) configuredDatabaseAliases() []string {
	aliases := make([]string, 0, len(d.databases))
	for _, database := range d.databases {
		aliases = append(aliases, database.Name)
	}
	return aliases
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

func (d *Driver) MaybeFromJSValue(ctx context.Context, jsvm *goja.Runtime, val goja.Value) (any, bool) {
	rtData := d.runtimeData[jsvm]
	if rtData == nil {
		return nil, false
	}
	if IsObjectId(jsvm, val, rtData.objectIdCtorVal) {
		return ObjectIdFromJSValue(jsvm, val), true
	}
	return nil, false
}

func (d *Driver) GetAppliedMigrationsMetadata(ctx context.Context) ([]*migrationsmeta.MigrationMetadata, error) {
	findOptions := options.Find().SetSort(bson.D{
		{Key: "filename", Value: 1},
	})
	cur, err := d.getMigrationsCollection().Find(ctx, bson.M{}, findOptions)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var migrationsMetadata []*migrationsmeta.MigrationMetadata
	if err := cur.All(ctx, &migrationsMetadata); err != nil {
		return nil, err
	}

	return migrationsMetadata, nil
}

func (d *Driver) SetAppliedMigrationsMetadata(ctx context.Context, migrationsMetadata []*migrationsmeta.MigrationMetadata) error {
	migrationsCollection := d.getMigrationsCollection()
	_, err := migrationsCollection.DeleteMany(ctx, bson.M{})
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
	_, err = migrationsCollection.InsertMany(ctx, documents)
	return err
}

func (d *Driver) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	session, err := d.client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(sessCtx mongo.SessionContext) (result any, returnErr error) {
		// Publish the active session so the JS-facing collection handles (both
		// the primary database and any sibling databases reached via db(name))
		// route their reads and writes through this transaction.
		d.sessionCtx = sessCtx
		defer func() { d.sessionCtx = nil }()

		// Recover from panics in the callback and convert to errors
		defer func() {
			if r := recover(); r != nil {
				if e, ok := r.(error); ok {
					returnErr = e
				} else {
					returnErr = fmt.Errorf("panic in transaction: %v", r)
				}
			}
		}()

		// Pass sessCtx as context.Context (mongo.SessionContext embeds context.Context)
		if err := fn(sessCtx); err != nil {
			return nil, err
		}
		return nil, nil
	})

	return err
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
