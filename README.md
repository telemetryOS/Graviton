# Graviton

<p>
  <img align="right" src="graviton.png" width="400" />
</p>

> The creator of light - forged in the limitless void

Graviton is a database-agnostic migration tool. Manage schema changes across MongoDB, PostgreSQL, MySQL, and SQLite using a single tool and consistent workflow. Write migrations in TypeScript and let Graviton handle execution, transaction management, and migration tracking regardless of which database you're using.

Most migration tools lock you into a single database technology. Graviton lets you use the right database for each part of your application while managing all migrations from one place with one command-line interface.

## Supported Databases

Graviton provides first-class support for four database systems, covering both SQL and NoSQL paradigms:

- **MongoDB** - Document-oriented NoSQL database
- **PostgreSQL** - Advanced open-source relational database
- **MySQL** - World's most popular open-source relational database
- **SQLite** - Serverless, embedded SQL database

Additionally, Graviton is compatible with database systems that use the same wire protocols: MariaDB works with the MySQL driver, and CockroachDB works with the PostgreSQL driver.

## Installation

```sh
go install github.com/telemetryos/graviton/cmd@latest
```

Alternatively, build from source or download a binary from the [releases page](https://github.com/telemetryos/graviton/releases).

## Quick Start

### Create a Configuration File

Graviton requires a configuration file named `graviton.config.toml` in your project root. This file defines which databases your project uses, where the single linear migration set lives, and which database holds the migration tracking.

```toml
migrations_db = "main"           # the [[databases]] entry that tracks applied migrations
migrations_path = "./migrations" # one directory, one linear ordered migration set

[[databases]]
name = "main"
kind = "postgresql"
connection_url = "postgres://user:pass@localhost:5432/mydb?sslmode=disable"
database_name = "mydb"
```

There is **one** migration directory and **one** linear applied-migrations list for the whole project, no matter how many databases are configured. `migrations_db` must name one of the configured `[[databases]]` entries; the tracking collection/table (`graviton-migrations`) is created there. When exactly one database is configured, `migrations_db` defaults to it and can be omitted.

### Create Your First Migration

Use the create command to generate a new migration file with the current timestamp. Migration files follow the naming pattern `TIMESTAMP-name.migration.ts`.

```bash
graviton create create-users-table
# Creates: migrations/20240106123045-create-users-table.migration.ts
```

### Apply Migrations

Run the up command to apply pending migrations. Migrations are executed in chronological order based on their timestamp. Each database a migration touches commits in its own transaction, and the applied-migration marker is written last (see [Migration Model](#migration-model)).

```bash
graviton up
```

## Writing Migrations

### Migration File Structure

All migration files must export two functions: `up` for applying changes and `down` for reversing them. The up function is called when applying a migration, and the down function is called when rolling back.

### MongoDB Migrations

MongoDB migrations interact with collections using a document-oriented API. The handle provides access to collection operations like insertOne, find, updateMany, and deleteOne. In a single-database project the root handle is bound directly to that database, so you can call `collection()` on it:

```typescript
export function up(g: Handle) {
  g.collection('users').insertOne({
    _id: new ObjectId('65b8077faddfba1bb64fa9fe'),
    name: 'Alice',
    email: 'alice@example.com',
    createdAt: new Date()
  })
}

export function down(g: Handle) {
  g.collection('users').deleteOne({
    _id: new ObjectId('65b8077faddfba1bb64fa9fe')
  })
}
```

### Selecting a Database with `use(alias)`

A migration reaches any configured database by its `[[databases]]` name with
`use(alias)`, which returns a handle bound to that database:

```typescript
export function up(g: Handle) {
  g.use('accounts').collection('accounts').insertOne({ name: 'Acme' })
  g.use('devices').collection('devices').insertOne({ name: 'lobby-screen' })
}
```

`use(alias)` is the single, uniform way to address databases; it subsumes the
old `db(name)` and `sibling(alias)` accessors, which have been removed.

- **Single-database projects** may call `collection()` (or the SQL surface)
  directly on the root handle — it is transparently bound to the one configured
  database. `use(alias)` still works there too.
- **Multi-database projects** must select a database with `use(alias)` first.
  Calling a direct operation on the root handle errors clearly, telling you which
  databases are configured and that `use(alias)` is required.
- An unknown alias errors cleanly, listing the configured aliases.

Writes across databases can be freely interleaved within one migration body.
This is the pattern for legacy → modern ETL migrations — read from one database,
write into another:

```typescript
export function up(g: Handle) {
  const legacyUsers = g.use('legacy').collection('users').find({})

  for (const legacyUser of legacyUsers) {
    g.use('accounts').collection('users').insertOne({
      _id: legacyUser._id,
      name: legacyUser.full_name,
      email: legacyUser.email_address,
      migratedAt: new Date()
    })
  }
}

export function down(g: Handle) {
  g.use('accounts').collection('users').deleteMany({ migratedAt: { $exists: true } })
}
```

Databases no longer need to be on the same cluster, and kinds can be mixed: each
database you touch transacts independently in its own driver (see
[Migration Model](#migration-model)). A MongoDB database and a PostgreSQL
database can be addressed from the same migration; each `use(alias)` handle
speaks its own kind's surface (`collection()` for MongoDB, `exec()`/`query()`/
`queryOne()` for SQL).

### SQL Migrations

SQL migrations for PostgreSQL, MySQL, and SQLite use a smart `sql` tag function that provides automatic parameterization and validation. The sql tag prevents SQL injection by automatically converting template literals into parameterized queries with proper placeholder syntax for each database.

```typescript
export function up(db: Handle) {
  db.exec(sql`
    CREATE TABLE users (
      id SERIAL PRIMARY KEY,
      name TEXT NOT NULL,
      email TEXT UNIQUE NOT NULL
    )
  `)

  const name = 'Alice'
  const email = 'alice@example.com'

  db.exec(sql`
    INSERT INTO users (name, email)
    VALUES (${name}, ${email})
  `)
}

export function down(db: Handle) {
  db.exec(sql`DROP TABLE users`)
}
```

### The sql Tag Function

The sql tag function is the recommended way to write SQL queries in Graviton migrations. It automatically handles three critical concerns: validating SQL syntax at migration load time, parameterizing user values to prevent SQL injection, and generating database-specific placeholder syntax.

When you write a query using the sql tag, Graviton analyzes the template literal and extracts the static SQL parts from the dynamic values. It then constructs a parameterized query appropriate for your database system, using numbered placeholders for PostgreSQL and question marks for MySQL and SQLite.

```typescript
// Template literal with dynamic values
db.exec(sql`SELECT * FROM users WHERE name = ${name}`)

// PostgreSQL: SELECT * FROM users WHERE name = $1
// MySQL/SQLite: SELECT * FROM users WHERE name = ?
// Params: ['Alice']
```

## Configuration

### Configuration File Format

Graviton uses TOML configuration files. Two top-level keys describe the migration set, and one or more `[[databases]]` tables describe the databases.

```toml
migrations_db = "main"
migrations_path = "./migrations"

[[databases]]
name = "main"
kind = "postgresql"
connection_url = "postgres://localhost:5432/mydb?sslmode=disable"
database_name = "mydb"
```

`${VAR}` references anywhere in the file are substituted from the environment
before parsing, keeping connection strings portable across environments.

### Top-Level Configuration

Configuration Field | Description
--------------------|------------
`migrations_db` | The `[[databases]]` `name` whose `graviton-migrations` collection/table holds the single linear applied-migrations list. Must reference a configured database. Defaults to the sole database when exactly one is configured.
`migrations_path` | Path to the one migration directory, relative to the config file. Defaults to `./migrations`.

### Database Configuration

Each `[[databases]]` entry requires a name for identification, a kind specifying the database type, a connection URL, and the database name to use. Migration files are no longer configured per database — there is one shared `migrations_path`.

Configuration Field | Description
--------------------|------------
`name` | Alias used by `use(alias)` and by `migrations_db`
`kind` | Database type: `mongodb`, `postgresql`, `mysql`, or `sqlite`
`connection_url` | Database connection string (format varies by database)
`database_name` | Name of the database to use

### Connection URLs

Connection URL formats vary by database system. PostgreSQL uses the postgres scheme with standard URL components. MySQL uses a custom format with the tcp protocol. SQLite uses file URLs pointing to database files. MongoDB uses the mongodb scheme with support for replica sets and authentication.

```toml
# PostgreSQL
connection_url = "postgres://user:pass@host:port/dbname?sslmode=disable"

# MySQL
connection_url = "user:pass@tcp(host:port)/dbname?parseTime=true"

# SQLite
connection_url = "file:./database.db?cache=shared&mode=rwc"

# MongoDB
connection_url = "mongodb://user:pass@host:port"
```

### Multi-Database Projects

Graviton manages multiple databases from one linear migration set. This is useful for applications that use different databases for different purposes, such as PostgreSQL for relational data and MongoDB for document storage, or for coordinated changes across several service databases.

```toml
migrations_db = "postgres-db"
migrations_path = "./migrations"

[[databases]]
name = "postgres-db"
kind = "postgresql"
connection_url = "postgres://localhost:5432/main"
database_name = "main"

[[databases]]
name = "mongo-db"
kind = "mongodb"
connection_url = "mongodb://localhost:27017"
database_name = "analytics"
```

There is no per-database command argument. Commands operate on the whole
project, and migrations pick databases with `use(alias)`:

```bash
graviton up
graviton status
```

A migration can touch both databases; each commits in its own transaction and
the marker is written last to `migrations_db`.

## Migration Model

Graviton uses a single linear migration set for the whole system: one migration
directory, one ordered list, and one applied-migrations record in
`migrations_db`. Migrations are ordered by their filename timestamp exactly as
before — there is just one directory now.

### Per-Handle Transactions and Marker-Last

Each database a migration touches gets its **own** session and transaction,
started lazily on its first operation and interleavable freely within the
migration body. When the body succeeds:

1. Every open data-database transaction commits, each independently.
2. **Then** the applied-migration marker is written to `migrations_db`, last, in
   its own transaction.

If a data commit fails, still-open transactions roll back, already-committed
databases stay committed, and the marker is **not** written. If the body errors
or panics, all open transactions roll back and no marker is written.

### Recovery Model: Idempotency + Re-run

Cross-database atomicity is **per handle, not joint** — this is deliberate.
Graviton does not attempt two-phase commit across heterogeneous databases.
Because the marker is written strictly last and in its own transaction, a process
that dies after some databases commit but before the marker is written leaves the
migration **unmarked**, so it re-runs on the next `up`.

The recovery model is therefore: **write idempotent/convergent migrations and
re-run them.** A migration that re-runs after a partial commit must converge to
the intended end state (e.g. use upserts, guard inserts with existence checks,
and make deletes match-by-predicate). Do not rely on joint atomicity across
databases.

### Mixing Kinds

Databases no longer need to share a cluster, and their kinds can differ. Each
`use(alias)` handle transacts within its own kind (a MongoDB transaction for a
`mongodb` entry, a SQL transaction for a SQL entry). The marker is written in
whatever kind `migrations_db` is.

## Commands

### up

The up command applies pending migrations in chronological order. Without arguments, it applies all pending migrations. With a target migration name, it applies only migrations up to and including that migration.

```bash
graviton up                      # Apply all pending migrations
graviton up create-users         # Apply up to and including a specific migration
```

Migrations apply one at a time in linear order. If a migration fails, previously applied migrations remain applied; the failed migration's data transactions roll back and its marker is not written, so it re-runs next time (see [Migration Model](#migration-model)).

### down

The down command rolls back applied migrations in reverse chronological order. It requires a target migration name and will roll back migrations down to and including that migration.

```bash
graviton down create-users       # Rollback to and including a migration
graviton down -                  # Rollback all migrations
```

Use the special `-` target to roll back all migrations.

### status

The status command displays the current state of the linear migration set. It shows which migrations have been applied and which are pending, helping you understand the current schema version.

```bash
graviton status         # Show applied and pending migrations
```

### set-head

The set-head command manually marks migrations as applied or unapplied without actually executing them. This is useful for testing migrations, skipping migrations that were manually applied, or resetting migration state.

```bash
graviton set-head create-users  # Mark up to migration as applied
graviton set-head -             # Mark all as unapplied
```

Use the special `-` target to mark all migrations as unapplied.

### create

The create command generates a new migration file with the current timestamp and provided name. The file is created with template up and down functions ready to be implemented.

```bash
graviton create add-users-table
# Creates: migrations/20240106123045-add-users-table.migration.ts
```

### upgrade

The upgrade command downloads and builds the latest version of Graviton from GitHub, replacing the current binary.

```bash
graviton upgrade
```

This fetches the latest release tag, clones the repository, builds a new binary, and replaces the current one.

## TypeScript API Reference

### MongoDB API

MongoDB migrations use a collection-based API that mirrors the MongoDB driver. Collections are accessed through the handle, and operations return results that can be used for further processing.

```typescript
interface Collection {
  insertOne(doc: any): { insertedID: string }
  insertMany(docs: any[]): { insertedIDs: string[] }
  findOne<T>(filter: any): T | null
  find<T>(filter: any): T[]
  updateOne(filter: any, update: any): void
  updateMany(filter: any, update: any): void
  deleteOne(filter: any): void
  deleteMany(filter: any): void
}

// A handle bound to a single configured database.
interface DbHandle {
  collection(name: string): Collection
}

interface Handle extends DbHandle {
  // Select any configured database by its [[databases]] name. Returns a new
  // bound handle instance per call. In a single-database project the root
  // handle is also bound directly, so collection() works without use().
  use(alias: string): DbHandle
}

declare class ObjectId {
  constructor(hexValue: string)
  toString(): string
  toHexString(): string
}
```

The ObjectId class is available globally for working with MongoDB object identifiers.

### SQL API

SQL migrations use a handle that provides three methods: exec for executing statements that modify data, query for retrieving multiple rows, and queryOne for retrieving a single row or null. All methods accept SQLQuery objects created by the sql tag function.

```typescript
interface SQLQuery {
  query: string      // SQL with placeholders
  params: any[]      // Bound values
  validated: boolean
}

interface SQLResult {
  rowsAffected: number
  lastInsertId: number  // MySQL/SQLite only (0 for PostgreSQL)
}

interface SqlDbHandle {
  exec(query: SQLQuery): SQLResult
  query<T = any>(query: SQLQuery): T[]
  queryOne<T = any>(query: SQLQuery): T | null
}

interface Handle extends SqlDbHandle {
  // Select any configured database by its [[databases]] name. In a single-SQL-
  // database project the root handle exposes exec/query/queryOne directly.
  use(alias: string): SqlDbHandle
}

declare function sql(
  strings: TemplateStringsArray,
  ...values: any[]
): SQLQuery
```

The sql tag function returns an SQLQuery object containing the parameterized query string, an array of parameter values, and a validation flag indicating the query was checked for syntax errors.

Query and queryOne methods support TypeScript generics for type-safe result handling.

```typescript
interface User {
  id: number
  name: string
  email: string
}

const users = db.query<User>(sql`SELECT * FROM users`)
// users: User[]

const user = db.queryOne<User>(sql`SELECT * FROM users WHERE id = ${id}`)
// user: User | null
```

## Best Practices

### Keep Migrations Focused

Each migration should address a single logical change to your database schema or data. Smaller, focused migrations are easier to understand, test, and debug. If a migration fails, you know exactly what went wrong and can fix it without affecting other changes.

```typescript
// ✅ Good: One table per migration
// 20240101-create-users.migration.ts
// 20240102-create-posts.migration.ts

// ❌ Avoid: Multiple unrelated changes
// 20240101-create-all-tables.migration.ts
```

### Always Provide Down Functions

Every migration should include a down function that reverses the changes made by the up function. This allows you to roll back changes if needed and makes it possible to test migrations by applying and rolling them back.

```typescript
export function up(db: Handle) {
  db.exec(sql`CREATE TABLE users (id SERIAL PRIMARY KEY, name TEXT)`)
}

export function down(db: Handle) {
  db.exec(sql`DROP TABLE users`)
}
```

If a migration truly cannot be reversed, omit the down function entirely rather than providing a no-op implementation.

### Don't Depend on External State

Migrations should be self-contained and not rely on data that exists outside of previous migrations. If your migration expects certain data to exist, that data should have been created by an earlier migration.

```typescript
// ❌ Bad: Depends on manually-added data
export function up(db: Handle) {
  const admin = db.queryOne(sql`SELECT * FROM users WHERE role = 'admin'`)
  // What if admin doesn't exist?
}

// ✅ Good: Self-contained
export function up(db: Handle) {
  db.exec(sql`INSERT INTO users (name, role) VALUES ('Admin', 'admin')`)
}
```

This ensures migrations can be run on fresh database instances and new environments without manual intervention.

### Transaction Behavior

Transactions are **per handle**, not joint across databases — see
[Migration Model](#migration-model) for the full contract. Within one migration,
each database you touch gets its own transaction, and on success they commit
independently before the applied marker is written. Design migrations to be
idempotent/convergent so that a re-run after a partial commit converges to the
intended state.

### SQL Injection Prevention

Always use the sql tag function when working with dynamic values in SQL migrations. Never concatenate user values directly into SQL strings, as this creates SQL injection vulnerabilities.

```typescript
// ❌ NEVER: String concatenation
const query = `SELECT * FROM users WHERE name = '${name}'`
db.exec(query)  // SQL injection risk!

// ✅ ALWAYS: Use sql tag
db.exec(sql`SELECT * FROM users WHERE name = ${name}`)
// Becomes: SELECT * FROM users WHERE name = $1
// Params: ['Alice']
```

The sql tag function automatically parameterizes all template literal values, ensuring they are safely escaped and bound to the query.

## Examples

The `example/` directory contains ready-to-read projects in the current
(v2) shape:

- `example/mongodb-migrations`, `example/postgresql-migrations`,
  `example/sqlite-migrations` — single-database projects.
- `example/multi-database-migrations` — two MongoDB databases addressed with
  `use(alias)` from one linear migration set.

> **Note:** The `neo-accounts-service/` directory at the repo root is
> **pre-v2** prior-art kept for reference. Its config still uses the old
> per-database `migrations_path` shape and predates `migrations_db`/`use()`; do
> not treat it as a template for new projects.

## Development

### Building from Source

Clone the repository and build with Go. The resulting binary can be placed anywhere in your PATH.

```bash
git clone https://github.com/telemetryos/graviton
cd graviton
go build -o graviton ./cmd/graviton
```

### Running Tests

Graviton includes comprehensive test coverage for all drivers. MongoDB tests require a MongoDB replica set running on localhost. PostgreSQL tests require PostgreSQL on localhost. MySQL tests require MySQL on localhost. SQLite tests use temporary files and require no external services.

```bash
# All tests
go test ./...

# Specific driver
go test ./driver/postgresql/...

# With verbose output
go test ./driver/mongodb/... -v
```

Tests verify transaction atomicity, error handling, panic recovery, and migration tracking across all database drivers.

## License

MIT License - see LICENSE file for details
