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
