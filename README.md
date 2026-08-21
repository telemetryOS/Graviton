# Graviton

<p>
  <img align="right" src="graviton.png" width="400" />
</p>

> The creator of light - forged in the limitless void

Graviton is a database-agnostic migration tool. Manage schema changes across MongoDB, PostgreSQL, MySQL, and SQLite — plus file and key-value stores (local filesystem, S3, Redis/Valkey) — using a single tool and consistent workflow. Write migrations in TypeScript and let Graviton handle execution, transaction management, and migration tracking regardless of which database you're using.

Most migration tools lock you into a single database technology. Graviton lets you use the right database for each part of your application while managing all migrations from one place with one command-line interface.

## Supported Databases

Graviton provides first-class support for four database systems, covering both SQL and NoSQL paradigms:

- **MongoDB** - Document-oriented NoSQL database
- **PostgreSQL** - Advanced open-source relational database
- **MySQL** - World's most popular open-source relational database
- **SQLite** - Serverless, embedded SQL database

Beyond databases, Graviton also migrates stores, so file layouts and cached
key-value state can evolve in the same linear migration set as the rows that
reference them:

- **fs** - A local filesystem directory (read, write, move, delete files)
- **s3** - An S3 bucket or prefix (get, put, copy, list, delete objects)
- **redis** - A Redis key-value store (strings, hashes, TTLs, any command)

Additionally, Graviton is compatible with systems that use the same wire
protocols or APIs: MariaDB works with the MySQL driver, CockroachDB works with
the PostgreSQL driver, Valkey works with the Redis driver, and S3-compatible
stores (MinIO, Cloudflare R2, DigitalOcean Spaces, …) work with the S3 driver.

## Installation

```sh
go install github.com/telemetryos/graviton/cmd@latest
```

Alternatively, build from source or download a binary from the [releases page](https://github.com/telemetryos/graviton/releases).

## Quick Start

### Create a Configuration File

By default, Graviton discovers a configuration file named
`graviton.config.toml` from the working directory or one of its parents. This
file defines which databases your project uses, where the single linear
migration set lives, and which database holds the migration tracking.

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

Every command also accepts a global `--config <path>` flag when discovery is
not appropriate. Relative config paths are resolved from the working directory;
the selected config's relative `migrations_path` is resolved from the directory
containing that config. An explicit path always wins over a discoverable
`graviton.config.toml`.

```bash
# Both flag positions are equivalent.
graviton --config ./config/development.toml status
graviton status --config ./config/development.toml

# Run an included example while staying at the repository root.
graviton --config ./example/multi-database-migrations/graviton.config.toml status
```

A missing, unreadable, or invalid explicit config fails before Graviton opens a
database connection.

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

### Retiring databases

When a database is fully cut over and no longer used, a migration can retire it
by renaming it out of the way rather than dropping it outright. `rename(newName)`
on a database-bound handle renames that database to `newName` — a **literal
physical name**, not a config alias: a MongoDB database name, an fs filesystem
path, or an s3 key prefix — and drops the emptied source. The convention is a
`__migrated__` suffix so the retired data is obvious and recoverable:

```typescript
export function up(g: Handle) {
  g.use('telemetry_v1').rename('telemetry_v1__migrated__')
}

export function down(g: Handle) {
  // Rename back to un-retire. The retired name must be reachable by alias, so
  // declare it as its own [[databases]] entry (see below).
  g.use('telemetry_v1_migrated').rename('telemetry_v1')
}
```

Per kind: MongoDB has no native database rename, so `rename` renames every
non-system collection to the target database with `renameCollection`, verifies
the source has no collections left, and drops it (same cluster, one client).
An fs database renames its root directory with one atomic filesystem move
(same filesystem only). An s3 database moves its configured key prefix by
server-side copying every object to the new prefix and then deleting the
sources — a database without a key prefix cannot be renamed, since a bucket
has no rename.

**Caveats — read before using it:**

- **It is non-transactional and immediate.** `rename` never opens or joins a
  transaction. It is **not** rolled back with the migration's data
  transactions: if the body fails *after* a `rename`, the rename stays. Keep a
  retire-databases migration dedicated to `rename` — do not mix it with
  transactional collection writes.
- **It is irreversible except by renaming back.** The source database is dropped
  once its contents have moved. To reverse it, `down()` renames the retired
  database back (see above).
- **No silent clobbering.** If the target (a collection, path, or prefix)
  already exists, the rename errors loudly rather than overwriting it.
- **It refuses unsafe targets.** Renaming the `migrations_db`, or a database that
  has an open transaction in the current run, errors cleanly.
- **The s3 rename is additionally non-atomic.** It is a mass copy followed by
  deletes; a failure mid-way leaves some objects copied and none deleted, and
  re-running converges.
- **`rename` is supported for mongodb, fs, and s3.** SQL and redis databases do
  not support it (for redis, rename key namespaces with `keys()` +
  `command('RENAME', …)` instead).

Because `rename` addresses its source by the bound handle's alias, a `down()`
that renames the retired database back needs that database reachable by an alias.
Declare the retired name as its own `[[databases]]` entry for the round-trip:

```toml
migrations_db = "tracking"

[[databases]]
name = "tracking"
kind = "mongodb"
connection_url = "mongodb://localhost:27017"
database_name = "tracking"

[[databases]]
name = "telemetry_v1"
kind = "mongodb"
connection_url = "mongodb://localhost:27017"
database_name = "telemetry_v1"

# Reachable so down() can rename it back to telemetry_v1.
[[databases]]
name = "telemetry_v1_migrated"
kind = "mongodb"
connection_url = "mongodb://localhost:27017"
database_name = "telemetry_v1__migrated__"
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

### Store Migrations (fs, s3, redis)

Store databases participate in migrations exactly like the others — select them
with `use(alias)` (or directly on the root handle in a single-database project)
and call their kind's surface. This makes coordinated data/file moves ordinary
migrations: relocate uploaded assets while rewriting the rows that reference
them, or rename cache namespaces alongside a schema change.

```typescript
export function up(g: Handle) {
  // Move each user's avatar into the new layout and update the row.
  for (const user of g.use('accounts').collection('users').find({})) {
    if (user.avatarPath) {
      const newPath = `avatars/${user._id}.png`
      g.use('uploads').move(user.avatarPath, newPath)
      g.use('accounts').collection('users').updateOne(
        { _id: user._id },
        { $set: { avatarPath: newPath } }
      )
    }
  }

  // Invalidate the cached profiles that embedded the old paths.
  g.use('cache').del(...g.use('cache').keys('profile:*'))
}
```

**fs** — the database is a root directory; paths are slash-separated and
sandboxed to it (`..` escapes fail the migration):
`read`/`readBytes`/`write`/`remove`/`removeAll`/`mkdir`/`list`/`exists`/`copy`/`move`.

**s3** — the database is a bucket, optionally under a key prefix; keys are
relative to that prefix: `get`/`getBytes`/`put`/`delete`/`list`/`exists`/
`copy`/`move`. `list(prefix)` treats the prefix as a folder path and follows
pagination.

**redis** — strings (`get`/`set` with optional TTL seconds/`del`/`keys`/
`exists`/`expire`/`ttl`), hashes (`hGet`/`hSet`/`hGetAll`/`hDel`), sets
(`sAdd`/`sRem`/`sMembers`/`sIsMember`), lists (`lPush`/`rPush`/`lRange`/
`lLen`), sorted sets (`zAdd`/`zRem`/`zRange`/`zScore`), counters
(`incr`/`incrBy`), plus `command(...)` for any Redis command verbatim
(`command('SETBIT', 'flags', 7, 1)`). `keys()` iterates with SCAN rather than
KEYS, so running against a large production store never blocks the server.

Binary content round-trips between file-like stores as `ArrayBuffer`:
`readBytes()`/`getBytes()` return one and `write()`/`put()` accept one, so
`g.use('bucket').put(key, g.use('files').readBytes(path))` copies bytes
faithfully.

For **large files**, `copyTo(destAlias, src, dst)` streams one item from an fs
or s3 database into another fs or s3 database in bounded memory — the
`readBytes()`/`put()` round trip above holds the whole payload at once, while
`copyTo` never does (s3 writes go through multipart upload as they grow):

```typescript
export function up(g: Handle) {
  for (const entry of g.use('files').list('videos')) {
    g.use('files').copyTo('bucket', `videos/${entry.name}`, `videos/${entry.name}`)
  }
}
```

**Store operations are immediate and non-transactional.** Filesystems and
object stores have no transactions, and Redis MULTI/EXEC cannot serve the
read-then-write pattern migrations rely on, so these three kinds apply every
operation as it executes. If the migration body fails afterwards, store writes
that already happened are **not** rolled back — the migration is simply left
unmarked and re-runs. This is the same recovery model as everything else in
Graviton (see [Migration Model](#migration-model)): write store migrations to
be idempotent/convergent, and prefer keeping heavy store work in dedicated
migrations rather than mixing it with transactional database writes.

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

Without `--config`, Graviton searches for `graviton.config.toml` from the
working directory upward. With `--config <path>`, only that file is loaded. A
relative flag value is working-directory-relative, while `migrations_path`
inside the selected file remains config-directory-relative.

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
`kind` | Database type: `mongodb`, `postgresql`, `mysql`, `sqlite`, `fs`, `s3`, or `redis`
`connection_url` | Database connection string (format varies by database)
`database_name` | Name of the database to use (unused by `fs`, `s3`, and `redis` — their connection URL carries the target)

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

# fs — the root directory (absolute, or relative to the working directory)
connection_url = "./store"

# s3 — bucket, optional key prefix, and options; credentials come from the
# standard AWS chain unless access-key/secret-key query params are given
# (endpoint/path-style enable MinIO, R2, and other S3-compatibles)
connection_url = "s3://my-bucket/app-assets?region=us-east-1"

# redis (or Valkey)
connection_url = "redis://user:pass@host:6379/0"
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

### The Migrations Lock

Commands that run migration bodies (`up`, `down`, `set-head`) first claim a
whole-run lock in `migrations_db`, next to the applied-migrations tracking
data — a `graviton-migrations-lock` collection/table, a
`graviton-migrations.lock` file/object, or a `graviton-migrations-lock` key,
depending on the tracking database's kind. One lock guards the whole project no
matter how many databases are configured, so two concurrent runs cannot
interleave migration bodies or clobber the tracking list; the second run exits
immediately, reporting who holds the lock and since when.

The lock is released when the run finishes, on success and failure alike. Only
a run that dies hard (kill -9, power loss) leaves it behind — clear that with
`graviton unlock` once you have confirmed the run is really dead. `status` is
read-only and takes no lock.

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

The store kinds (`fs`, `s3`, `redis`) have no transactions at all: their
operations apply immediately and are never rolled back (see
[Store Migrations](#store-migrations-fs-s3-redis)). They still fit the same
recovery model — a failed body leaves the migration unmarked, so it re-runs —
and any of them can serve as `migrations_db` (the tracking list is stored as a
`graviton-migrations.json` file/object, or a `graviton-migrations` key).

## Commands

`--config <path>` is a global option and may appear before or after any
subcommand. Omit it to retain `graviton.config.toml` discovery.

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

### unlock

The unlock command clears the whole-run migrations lock (see
[The Migrations Lock](#the-migrations-lock)) after a run died without releasing
it. It prints who held the lock and since when, then removes it. Never unlock
while a migration run is still alive — the lock is what keeps concurrent runs
from corrupting migration tracking.

```bash
graviton unlock
# Clearing migrations lock held by ci-runner-3 (pid 4242) since 2024-01-06T12:30:45Z
# Migrations lock cleared
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
  // Rename this database to a literal physical name and drop the source. See
  // "Retiring databases". Immediate and non-transactional.
  rename(newName: string): void
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

### Environment, encoding and crypto

Three globals are available to every migration regardless of which databases it
touches, because they are properties of the runtime rather than of a driver.

```typescript
type Bytes = ArrayBuffer

interface Env {
  // Throws naming the variable when unset. An empty value flowing into a
  // decrypt or a comparison fails far from its cause.
  get(name: string): string
  has(name: string): boolean
}

interface Enc {
  decode(encoding: "base64" | "base64url" | "hex" | "utf8", text: string): Bytes
  encode(encoding: "base64" | "base64url" | "hex" | "utf8", bytes: Bytes): string
}

interface Crypto {
  // Symmetric AEAD and public-key encryption share these two functions; the
  // algorithm decides how the key is read. nonce is optional for AEAD — pass
  // one for reproducible output, omit it for a random one.
  decrypt(algorithm: EncryptAlgorithm, key: Bytes, ciphertext: Bytes, nonce?: Bytes): Bytes
  encrypt(algorithm: EncryptAlgorithm, key: Bytes, plaintext: Bytes, nonce?: Bytes): Bytes

  hash(algorithm: HashAlgorithm, data: Bytes): Bytes
  hmac(algorithm: HashAlgorithm, key: Bytes, data: Bytes): Bytes

  random(length: number): Bytes
  generateKeyPair(algorithm: KeyPairAlgorithm): { publicKey: Bytes, privateKey: Bytes }

  sign(algorithm: SignAlgorithm, privateKey: Bytes, data: Bytes): Bytes
  verify(algorithm: SignAlgorithm, publicKey: Bytes, data: Bytes, signature: Bytes): boolean

  exchange(algorithm: ExchangeAlgorithm, privateKey: Bytes, peerPublicKey: Bytes): Bytes
  derive(algorithm: DeriveAlgorithm, secret: Bytes, salt: Bytes, info: Bytes, length: number): Bytes

  password: {
    hash(algorithm: "bcrypt" | "argon2id", password: Bytes): string
    verify(algorithm: "bcrypt" | "argon2id", password: Bytes, encoded: string): boolean
  }
}

// encrypt/decrypt  aes-gcm, chacha20-poly1305, rsa-oaep-sha256, rsa-oaep-sha512
// hash/hmac        sha256, sha384, sha512, sha3-256, sha3-512, sha1
// generateKeyPair  ed25519, x25519, ecdsa-p256/384/521, rsa-2048/3072/4096
// sign/verify      ed25519, ecdsa-sha256/384/512, rsa-pss-sha256, rsa-pkcs1-sha256
// exchange         x25519, ecdh-p256/384/521
// derive           hkdf-sha256/512, pbkdf2-sha256/512, scrypt, argon2id
```

Reading data that a legacy service wrote encrypted:

```typescript
const key = enc.decode("base64", env.get("EDM_AES_KEY_V1"))
const raw = enc.decode("base64", row.url)
const url = JSON.parse(enc.encode("utf8", crypto.decrypt("aes-gcm", key, raw)))
```

The algorithm comes first so a call names the operation it performs, and an
unknown algorithm or encoding throws listing the supported set — a typo that
silently fell back would be written to the target database before anyone
noticed. Keys and data are always bytes; `enc` performs every conversion, so
nothing has to infer whether an argument arrived encoded.

Decryption is authenticated. A wrong key or altered ciphertext throws rather
than returning wrong plaintext. For AEAD the nonce is read from the front of
the ciphertext, where `encrypt` writes it — or passed as a fourth argument when
the stored format keeps it in a separate field, which many do. AES keys may be
16, 24 or 32 bytes.

`encrypt` takes an optional nonce. Supplying one makes the output reproducible,
so re-running a migration leaves unchanged rows alone; omitting it generates a
random nonce, so the same input encrypts differently each run. Which of those a
migration needs is its author's decision.

Key pairs come back as two independent byte values, PKCS#8 for the private key
and SPKI for the public one, so a key a migration writes is readable by any
other language. Generating one produces a new value on every call — a migration
that must not mint a second identity should check whether the key already
exists before generating.

```typescript
const pair = crypto.generateKeyPair("ed25519")
const sig  = crypto.sign("ed25519", pair.privateKey, enc.decode("utf8", body))
const ok   = crypto.verify("ed25519", pair.publicKey, enc.decode("utf8", body), sig)
```

`password.hash` and `password.verify` are kept apart from `derive` deliberately.
They use the same primitives for a different job, and their output is a
self-describing string carrying its own cost parameters, so a stored hash stays
verifiable after the defaults here change. Reach for `derive` when you need key
material, and `password` when you need to check a secret someone typed.

Key material is an ordinary string once `env.get` returns it, so it can be
logged or serialised like any other value. Keep it out of `console.log` and out
of anything written back to a database.

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

### Store API

The store kinds share the same `use(alias)` handle model. All operations are
immediate and non-transactional (see
[Store Migrations](#store-migrations-fs-s3-redis)).

```typescript
interface FileEntry {
  name: string
  isDir: boolean
  size: number
}

// fs — paths are slash-separated and relative to the configured root
interface FsDbHandle {
  read(path: string): string
  readBytes(path: string): ArrayBuffer
  write(path: string, data: string | ArrayBuffer): void
  remove(path: string): void        // one file or empty directory
  removeAll(path: string): void     // recursive; missing paths are fine
  mkdir(path: string): void
  list(path: string): FileEntry[]
  exists(path: string): boolean
  copy(src: string, dst: string): void
  move(src: string, dst: string): void
  rename(newName: string): void     // retire: move the root directory
  copyTo(destAlias: string, src: string, dst: string): void  // stream to fs/s3
}

// s3 — keys are relative to the configured bucket prefix
interface S3DbHandle {
  get(key: string): string          // fails on a missing key
  getBytes(key: string): ArrayBuffer
  put(key: string, data: string | ArrayBuffer): void
  delete(key: string): void         // missing keys are fine (S3 semantics)
  list(prefix: string): string[]    // folder-path prefix; '' lists everything
  exists(key: string): boolean
  copy(src: string, dst: string): void
  move(src: string, dst: string): void
  rename(newPrefix: string): void   // retire: move the key prefix (non-atomic)
  copyTo(destAlias: string, src: string, dst: string): void  // stream to fs/s3
}

// redis — also Valkey
interface RedisDbHandle {
  get(key: string): string | null   // null on a miss
  set(key: string, value: any, ttlSeconds?: number): void
  del(...keys: string[]): number
  keys(pattern: string): string[]   // SCAN-based; never blocks the server
  exists(key: string): boolean
  expire(key: string, seconds: number): boolean
  ttl(key: string): number          // -1 no expiry, -2 missing
  hGet(key: string, field: string): string | null
  hSet(key: string, field: string, value: any): void
  hGetAll(key: string): Record<string, string>
  hDel(key: string, ...fields: string[]): number
  sAdd(key: string, ...members: any[]): number
  sRem(key: string, ...members: any[]): number
  sMembers(key: string): string[]
  sIsMember(key: string, member: any): boolean
  lPush(key: string, ...values: any[]): number
  rPush(key: string, ...values: any[]): number
  lRange(key: string, start: number, stop: number): string[]
  lLen(key: string): number
  zAdd(key: string, score: number, member: string): boolean
  zRem(key: string, ...members: any[]): number
  zRange(key: string, start: number, stop: number): string[]
  zScore(key: string, member: string): number | null
  incr(key: string): number
  incrBy(key: string, delta: number): number
  command(...args: any[]): any      // any Redis command verbatim
}
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
- `example/fs-migrations`, `example/s3-migrations`, `example/redis-migrations`
  — single-store projects (file layouts and key-value state).
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

Graviton includes comprehensive test coverage for all drivers. MongoDB tests require a MongoDB replica set running on localhost. PostgreSQL tests require PostgreSQL on localhost. MySQL tests require MySQL on localhost. SQLite and fs tests use temporary files and require no external services. Redis tests require a Redis/Valkey on localhost (they use logical database 15; `GRAVITON_TEST_REDIS_URL` overrides the target). S3 tests run against an in-memory fake; set `GRAVITON_TEST_S3_URL` to also exercise a real S3-compatible endpoint such as a local MinIO. Tests whose backing service is unavailable skip rather than fail.

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
