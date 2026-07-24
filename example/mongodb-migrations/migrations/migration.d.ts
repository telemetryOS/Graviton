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

// A handle bound to a single configured database. use(alias) returns one of
// these for any database in graviton.config.toml.
type DbHandle = {
  collection: (name: string) => Collection;
  // Rename this database to newName (a literal physical name) and drop the
  // source. Intended for retiring a database at cutover — e.g. rename it with a
  // "__migrated__" suffix. The rename is immediate and NON-transactional: it is
  // not part of the migration's transactions and is not rolled back if the body
  // later fails. It is irreversible except by renaming back (do that from
  // down()). Errors if a target collection already exists, if the database has
  // an open transaction in this run, or on the migrations_db.
  rename: (newName: string) => void;
}

// The root handle passed to up()/down(). use(alias) selects any configured
// database by its [[databases]] name. In a single-database project the root
// handle is also bound directly to that database, so collection() may be called
// on it without use(). In a multi-database project, use(alias) is required.
type Handle = DbHandle & {
  use: (alias: string) => DbHandle;
}

type Console = {
  log(...args: any[]): void;
}
declare const console: Console;

declare class ObjectId {
  constructor(hexValue: string);
  toHexString(): string;
  toString(): string;
}
