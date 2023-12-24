package mongodb

import (
	"context"

	"github.com/telemetrytv/graviton-cli/internal/driver"
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
	client   *mongo.Client
	database *mongo.Database
}

func New() *Driver {
	return &Driver{}
}

func (d *Driver) Connect(ctx context.Context, opts *Options) error {
	clientOptions := options.Client().
		ApplyURI(opts.URI)

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return err
	}

	d.client = client
	d.database = client.Database(opts.Database)

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

func (d *Driver) GetAppliedMigrationsMetadata(ctx context.Context) ([]*driver.MigrationMetadata, error) {
	findOptions := options.Find().SetSort(bson.D{
		{Key: "filename", Value: 1},
	})
	cur, err := d.getMigrationsCollection().Find(ctx, bson.M{}, findOptions)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var migrationsMetadata []*driver.MigrationMetadata
	if err := cur.All(ctx, &migrationsMetadata); err != nil {
		return nil, err
	}

	return migrationsMetadata, nil
}

func (d *Driver) SetAppliedMigrationsMetadata(ctx context.Context, migrationsMetadata []*driver.MigrationMetadata) error {
	session, err := d.client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)

	session.WithTransaction(ctx, func(ctx mongo.SessionContext) (any, error) {
		migrationsCollection := d.getMigrationsCollection()
		migrationsCollection.DeleteMany(ctx, bson.M{})
		var documents []any
		for _, migrationMetadata := range migrationsMetadata {
			documents = append(documents, migrationMetadata)
		}
		migrationsCollection.InsertMany(ctx, documents)
		return nil, nil
	})

	return nil
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
