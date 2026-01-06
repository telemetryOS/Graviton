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

Graviton requires a configuration file named `graviton.config.toml` in your project root. This file defines which databases your project uses and where migration files are located.

```toml
[[databases]]
name = "main"
kind = "postgresql"
connection_url = "postgres://user:pass@localhost:5432/mydb?sslmode=disable"
database_name = "mydb"
migrations_path = "migrations"
```

### Create Your First Migration

Use the create command to generate a new migration file with the current timestamp. Migration files follow the naming pattern `TIMESTAMP-name.migration.ts`.

```bash
graviton create create-users-table
# Creates: migrations/20240106123045-create-users-table.migration.ts
```

### Apply Migrations

Run the up command to apply pending migrations. Migrations are executed in chronological order based on their timestamp. Each migration runs in its own transaction, ensuring that partial application is possible if a later migration fails.

```bash
graviton up
```

## Writing Migrations

### Migration File Structure

All migration files must export two functions: `up` for applying changes and `down` for reversing them. The up function is called when applying a migration, and the down function is called when rolling back.

### MongoDB Migrations

MongoDB migrations interact with collections using a document-oriented API. The handle provides access to collection operations like insertOne, find, updateMany, and deleteOne.

```typescript
export function up(db: Handle) {
  db.collection('users').insertOne({
    _id: new ObjectId('65b8077faddfba1bb64fa9fe'),
    name: 'Alice',
    email: 'alice@example.com',
    createdAt: new Date()
  })
}

export function down(db: Handle) {
  db.collection('users').deleteOne({
    _id: new ObjectId('65b8077faddfba1bb64fa9fe')
  })
}
```

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

Graviton uses TOML configuration files. The configuration can define one or more databases, making it possible to manage multiple database systems within a single project.

```toml
[[databases]]
name = "main"
kind = "postgresql"
connection_url = "postgres://localhost:5432/mydb?sslmode=disable"
database_name = "mydb"
migrations_path = "migrations"
```

### Database Configuration

Each database configuration requires a name for identification, a kind specifying the database type, a connection URL with credentials and connection parameters, the database name to use, and a path to the migration files.

Configuration Field | Description
--------------------|------------
`name` | Identifier used in CLI commands to target this database
`kind` | Database type: `mongodb`, `postgresql`, `mysql`, or `sqlite`
`connection_url` | Database connection string (format varies by database)
`database_name` | Name of the database to use
`migrations_path` | Path to migration files relative to config file

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

Graviton supports managing multiple databases in a single project. This is useful for applications that use different databases for different purposes, such as PostgreSQL for relational data and MongoDB for document storage.

```toml
[[databases]]
name = "postgres-db"
kind = "postgresql"
connection_url = "postgres://localhost:5432/main"
database_name = "main"
migrations_path = "migrations/postgres"

[[databases]]
name = "mongo-db"
kind = "mongodb"
connection_url = "mongodb://localhost:27017"
database_name = "analytics"
migrations_path = "migrations/mongo"
```

When multiple databases are configured, you must specify which database to target in commands using the database name parameter.

```bash
graviton up postgres-db
graviton status mongo-db
```

## Commands

### up

The up command applies pending migrations in chronological order. Without arguments, it applies all pending migrations. With a target migration name, it applies only migrations up to and including that migration.

```bash
graviton up                      # Apply all pending migrations
graviton up create-users         # Apply up to specific migration
graviton up mydb                 # Apply for specific database
graviton up mydb create-users    # Apply to migration on specific database
```

Each migration executes in its own transaction. If a migration fails, previous migrations remain applied and only the failed migration is rolled back.

### down

The down command rolls back applied migrations in reverse chronological order. It requires a target migration name and will roll back migrations down to and including that migration.

```bash
graviton down create-users       # Rollback to and including migration
graviton down -                  # Rollback all migrations
graviton down mydb create-users  # Rollback on specific database
```

Use the special `-` target to roll back all migrations.

### status

The status command displays the current state of migrations for a database. It shows which migrations have been applied and which are pending, helping you understand the current schema version.

```bash
graviton status         # Show status for default database
graviton status mydb    # Show status for specific database
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

interface Handle {
  collection(name: string): Collection
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

interface Handle {
  exec(query: SQLQuery): SQLResult
  query<T = any>(query: SQLQuery): T[]
  queryOne<T = any>(query: SQLQuery): T | null
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

Each migration runs in its own transaction. If migration 5 fails, migrations 1-4 remain committed to the database. This allows for incremental progress and makes it easier to fix issues without losing work.

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

## Development

### Building from Source

Clone the repository and build with Go. The resulting binary can be placed anywhere in your PATH.

```bash
git clone https://github.com/telemetryos/graviton
cd graviton
go build -o graviton ./cmd
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
