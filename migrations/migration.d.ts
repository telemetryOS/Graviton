type InsertManyResult = {
  insertedIDs: string[];
}

type InsertOneResult = {
  insertedID: string;
}

type Collection = {
  insertMany(docs: Record<string, any>[]): InsertManyResult;
  insertOne(docs: Record<string, any>): InsertOneResult;
  find<Document = Record<string, any>[]>(filter: Record<string, any>): Document[];
  findOne<Document = Record<string, any>[]>(filter: Record<string, any>): Document;
  updateMany(filter: Record<string, any>, update: Record<string, any>): void;
  updateOne(filter: Record<string, any>, update: Record<string, any>): void;
  deleteMany(filter: Record<string, any>): void;
  deleteOne(filter: Record<string, any>): void;
}

// One list() result on an fs database.
type FileEntry = {
  name: string;
  isDir: boolean;
  size: number;
}

// A handle bound to a single configured database. use(alias) returns one of
// these for any database in graviton.config.toml. Each operation is available
// only on the database kinds noted for it; calling it on another kind fails
// the migration with a clear error.
//
// Store kinds (fs, s3, redis) have NO transactions: their operations apply
// immediately and are not rolled back if the migration body later fails.
// Write store migrations to be idempotent/convergent so a re-run after a
// partial failure is safe.
type DbHandle = {
  // ---- mongodb ----
  collection: (name: string) => Collection;
  // Rename this database to newName — a literal physical name: a mongodb
  // database name, an fs filesystem path, or an s3 key prefix — and drop the
  // source. Intended for retiring a database at cutover, e.g. rename it with a
  // "__migrated__" suffix. The rename is immediate and NON-transactional: it is
  // not part of the migration's transactions and is not rolled back if the body
  // later fails. It is irreversible except by renaming back (do that from
  // down()). Errors when the target already exists, when the database has an
  // open transaction in this run, or on the migrations_db. Supported for
  // mongodb, fs, and s3 databases.
  rename: (newName: string) => void;
  // Stream one item from this database into another configured fs or s3
  // database without buffering the whole payload — the way to move large
  // files between stores. Immediate and non-transactional.
  copyTo: (destAlias: string, src: string, dst: string) => void;

  // ---- fs (paths are slash-separated, relative to the configured root) ----
  read: (path: string) => string;
  readBytes: (path: string) => ArrayBuffer;
  write: (path: string, data: string | ArrayBuffer) => void;
  remove: (path: string) => void;
  removeAll: (path: string) => void;
  mkdir: (path: string) => void;
  list: (path: string) => FileEntry[];
  copy: (src: string, dst: string) => void;
  move: (src: string, dst: string) => void;

  // ---- s3 (keys are relative to the configured bucket prefix) ----
  // list(prefix) on an s3 database returns object keys (string[]), treating
  // prefix as a folder path; exists/copy/move are shared with fs above.
  // get() on an s3 database fails on a missing key; on a redis database it
  // returns null.
  get: (key: string) => string | null;
  getBytes: (key: string) => ArrayBuffer;
  put: (key: string, data: string | ArrayBuffer) => void;
  delete: (key: string) => void;
  exists: (pathOrKey: string) => boolean;

  // ---- redis (also Valkey) ----
  set: (key: string, value: any, ttlSeconds?: number) => void;
  del: (...keys: string[]) => number;
  // keys() iterates with SCAN, so large stores are never blocked.
  keys: (pattern: string) => string[];
  expire: (key: string, seconds: number) => boolean;
  ttl: (key: string) => number;
  hGet: (key: string, field: string) => string | null;
  hSet: (key: string, field: string, value: any) => void;
  hGetAll: (key: string) => Record<string, string>;
  hDel: (key: string, ...fields: string[]) => number;
  sAdd: (key: string, ...members: any[]) => number;
  sRem: (key: string, ...members: any[]) => number;
  sMembers: (key: string) => string[];
  sIsMember: (key: string, member: any) => boolean;
  lPush: (key: string, ...values: any[]) => number;
  rPush: (key: string, ...values: any[]) => number;
  lRange: (key: string, start: number, stop: number) => string[];
  lLen: (key: string) => number;
  zAdd: (key: string, score: number, member: string) => boolean;
  zRem: (key: string, ...members: any[]) => number;
  zRange: (key: string, start: number, stop: number) => string[];
  zScore: (key: string, member: string) => number | null;
  incr: (key: string) => number;
  incrBy: (key: string, delta: number) => number;
  // Any Redis command verbatim, e.g. command('SETBIT', 'flags', 7, 1).
  command: (...args: any[]) => any;
}

// The root handle passed to up()/down(). use(alias) selects any configured
// database by its [[databases]] name. In a single-database project the root
// handle is also bound directly to that database, so collection() (or the
// kind's surface) may be called on it without use(). In a multi-database
// project, use(alias) is required.
type Handle = DbHandle & {
  use: (alias: string) => DbHandle;
}

type Console = {
  log(...args: any[]): void;
}
declare const console: Console;

// Raw bytes. Every crypto and enc function takes and returns these, so nothing
// has to guess whether a value arrived encoded or not.
type Bytes = ArrayBuffer;

type Encoding = "base64" | "base64url" | "hex" | "utf8";

// Conversion between encoded strings and bytes. Separate from crypto because
// migrations meet base64 blobs and hex ids in plenty of places that have
// nothing to do with keys. An unknown encoding throws rather than guessing.
type Enc = {
  decode(encoding: Encoding, text: string): Bytes;
  encode(encoding: Encoding, bytes: Bytes): string;
}
declare const enc: Enc;

// Process environment. get() throws naming the variable when it is unset — an
// empty value flowing into a decrypt or a comparison fails far from its cause.
type Env = {
  get(name: string): string;
  has(name: string): boolean;
}
declare const env: Env;

type AeadAlgorithm = "aes-gcm" | "chacha20-poly1305";
type HashAlgorithm = "sha256" | "sha512" | "sha1";

// Algorithm first, so a call names the operation it performs. Decryption is
// authenticated: a wrong key or altered ciphertext throws rather than yielding
// wrong plaintext. The nonce is expected at the front of the ciphertext, and
// encrypt() writes it there.
//
// encrypt() requires an explicit nonce. A random one would make a migration
// non-convergent — re-running it would rewrite unchanged rows with different
// ciphertext and break provenance comparison — so callers derive one
// deterministically from what they are encrypting.
type Crypto = {
  decrypt(algorithm: AeadAlgorithm, key: Bytes, ciphertext: Bytes): Bytes;
  encrypt(algorithm: AeadAlgorithm, key: Bytes, plaintext: Bytes, nonce: Bytes): Bytes;
  hash(algorithm: HashAlgorithm, data: Bytes): Bytes;
  hmac(algorithm: HashAlgorithm, key: Bytes, data: Bytes): Bytes;
}
declare const crypto: Crypto;

declare class ObjectId {
  constructor(hexValue: string);
  toHexString(): string;
  toString(): string;
}
