type InsertManyResult = {
  insertedIDs: string[];
}

type InsertOneResult = {
  insertedID: string;
}

type ObjectId = {
  new(hexValue: string): ObjectId;
  toHexString(): string;
  toString(): string;
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

type Handle = {
  collection: (name: string) => Collection;
}
