package mongodb

import (
	"context"
	"errors"
	"fmt"

	"github.com/dop251/goja"
	"github.com/telemetrytv/graviton-cli/internal/config"
	migrationsmeta "github.com/telemetrytv/graviton-cli/internal/migrations-meta"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const MIGRATIONS_COLLECTION = "graviton-migrations"

type Options struct {
	URI      string
	Database string
}

type Driver struct {
	config   *config.DatabaseConfig
	client   *mongo.Client
	database *mongo.Database
}

func New(conf *config.DatabaseConfig) *Driver {
	return &Driver{config: conf}
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
	if buildInfo.Version < "4.0" {
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

func (d *Driver) Globals(ctx context.Context) map[string]any {
	globals := map[string]any{}
	globals["ObjectId"] = JSObjectIdCtor
	return globals
}

func (d *Driver) MaybeFromJSValue(ctx context.Context, jsvm *goja.Runtime, val goja.Value) (any, bool) {
	if IsObjectId(jsvm, val) {
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
	session, err := d.client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(ctx mongo.SessionContext) (any, error) {
		migrationsCollection := d.getMigrationsCollection()
		migrationsCollection.DeleteMany(ctx, bson.M{})
		var documents []any
		for _, migrationMetadata := range migrationsMetadata {
			documents = append(documents, migrationMetadata)
		}
		_, err := migrationsCollection.InsertMany(ctx, documents)
		if err != nil {
			return nil, err
		}
		return nil, nil
	})

	return err
}

func (d *Driver) WithTransaction(ctx context.Context, fn func() error) error {
	session, err := d.client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)

	session.WithTransaction(ctx, func(ctx mongo.SessionContext) (any, error) {
		if err := fn(); err != nil {
			return nil, err
		}
		return nil, nil
	})

	return nil
}

func (d *Driver) getMigrationsCollection() *mongo.Collection {
	return d.database.Collection(MIGRATIONS_COLLECTION)
}
