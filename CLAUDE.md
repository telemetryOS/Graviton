# Graviton

This file provides guidance to AI coding agents (Claude Code, Cursor, etc.) when working with code in this repository.

Graviton is a database-agnostic migration tool that manages schema changes across MongoDB, PostgreSQL, MySQL, and SQLite using TypeScript migration files executed via an embedded JavaScript runtime (goja).

## Build & Test Commands

```bash
# Build
go build -o graviton ./cmd/graviton

# Run all tests
go test ./...

# Run tests for a specific driver
go test ./driver/postgresql/...
go test ./driver/mongodb/...

# Run single test
go test ./driver/mongodb/... -run Test_Driver_Connect -v

# Install dependencies
go get -v -t ./...
```

## Architecture

### Driver System

The `driver/` package implements a plugin architecture for database support:

- `driver/driver.go` - Defines the `Driver` interface that all backends implement
- `driver/{mongodb,postgresql,mysql,sqlite}/` - Database-specific implementations

Each driver must implement: `Connect`, `Disconnect`, `WithTransaction`, `Handle`, `Init`, `Globals`, and migration metadata operations.

### Migration Execution Flow

1. Config loaded from `graviton.config.toml` (supports env var substitution via `${VAR}`)
2. Driver selected based on `kind` field in config
3. TypeScript migrations transpiled via esbuild, executed via goja runtime
4. Driver's `Handle()` injected into JS runtime for database operations
5. Each migration runs in its own transaction via `WithTransaction()`

### SQL vs MongoDB APIs

**SQL drivers** (PostgreSQL, MySQL, SQLite): Provide `exec()`, `query()`, `queryOne()` methods. Use the `sql` tag function for parameterized queries - it generates `$1, $2` for PostgreSQL or `?` for MySQL/SQLite.

**MongoDB driver**: Provides collection-based API (`collection().insertOne()`, `find()`, etc.) with `ObjectId` global.

## Key Patterns

### Adding a New Driver

1. Create `driver/newdb/` with `driver.go`, `handle.go`, `driver_test.go`
2. Implement all `Driver` interface methods
3. Add case in `driver/driver.go:FromDatabaseConfig()`
4. Add `DatabaseKindX` constant in `config/config.go`
5. For SQL drivers: Add SQL files in `sql/` subdirectory, use `sql_tag.go` pattern

### Transaction Context Pattern

Drivers pass transaction context through `context.Context`. Within `WithTransaction()`, operations must use the provided context to participate in the transaction:

```go
drv.WithTransaction(ctx, func(txCtx context.Context) error {
    // Use txCtx for all operations
    return drv.SetAppliedMigrationsMetadata(txCtx, migrations)
})
```

### Test Database Requirements

- MongoDB tests require a replica set on `localhost:27017` (transactions need replica set)
- PostgreSQL tests require server on `localhost:5432`
- SQLite tests use in-memory databases, no external deps
- Tests skip gracefully if database unavailable (`t.Skipf`)

## Gotchas

- MongoDB transactions require replica set mode - standalone mongod won't work for tests
- The `sql` tag function validates queries at migration load time, not just execution
- Migration filenames must match pattern `TIMESTAMP-name.migration.ts`
- Config searches upward from cwd for `graviton.config.toml`
- Panics inside migration transactions are caught and converted to errors with rollback
