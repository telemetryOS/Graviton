// One list() result.
type FileEntry = {
  name: string;
  isDir: boolean;
  size: number;
}

// A handle bound to a single configured fs database (a root directory). Paths
// are slash-separated and relative to that root; escaping it with .. fails the
// migration. Operations apply immediately — fs databases have no
// transactions — so write migrations to be idempotent.
type DbHandle = {
  read(path: string): string;
  readBytes(path: string): ArrayBuffer;
  write(path: string, data: string | ArrayBuffer): void;
  remove(path: string): void;      // one file or empty directory
  removeAll(path: string): void;   // recursive; missing paths are fine
  mkdir(path: string): void;
  list(path: string): FileEntry[];
  exists(path: string): boolean;
  copy(src: string, dst: string): void;
  move(src: string, dst: string): void;
  // Retire this database by moving its root directory to a literal path
  // (e.g. a "__migrated__" suffix). Immediate and non-transactional.
  rename(newName: string): void;
  // Stream one file into another configured fs or s3 database without
  // buffering the whole payload — for large files.
  copyTo(destAlias: string, src: string, dst: string): void;
}

// The root handle passed to up()/down(). In this single-database project it is
// bound directly to the fs database; use(alias) also works.
type Handle = DbHandle & {
  use: (alias: string) => DbHandle;
}

type Console = {
  log(...args: any[]): void;
}
declare const console: Console;
