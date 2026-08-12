// A handle bound to a single configured s3 database (a bucket, optionally
// under a key prefix). Keys are relative to that prefix. Operations apply
// immediately — object stores have no transactions — so write migrations to
// be idempotent.
type DbHandle = {
  get(key: string): string;        // fails on a missing key
  getBytes(key: string): ArrayBuffer;
  put(key: string, data: string | ArrayBuffer): void;
  delete(key: string): void;       // missing keys are fine (S3 semantics)
  list(prefix: string): string[];  // folder-path prefix; '' lists everything
  exists(key: string): boolean;
  copy(src: string, dst: string): void;
  move(src: string, dst: string): void;
  // Retire this database by moving every object to a new key prefix in the
  // same bucket (e.g. a "__migrated__" suffix). Immediate, non-transactional,
  // and non-atomic (mass copy + delete); re-running converges.
  rename(newPrefix: string): void;
  // Stream one object into another configured fs or s3 database without
  // buffering the whole payload — for large objects.
  copyTo(destAlias: string, src: string, dst: string): void;
}

// The root handle passed to up()/down(). In this single-database project it is
// bound directly to the s3 database; use(alias) also works.
type Handle = DbHandle & {
  use: (alias: string) => DbHandle;
}

type Console = {
  log(...args: any[]): void;
}
declare const console: Console;
