// A handle bound to a single configured redis (or Valkey) database.
// Operations apply immediately — the driver does not use MULTI/EXEC — so
// write migrations to be idempotent.
type DbHandle = {
  get(key: string): string | null;
  set(key: string, value: any, ttlSeconds?: number): void;
  del(...keys: string[]): number;
  // keys() iterates with SCAN, so large stores are never blocked.
  keys(pattern: string): string[];
  exists(key: string): boolean;
  expire(key: string, seconds: number): boolean;
  ttl(key: string): number;        // -1 no expiry, -2 missing
  hGet(key: string, field: string): string | null;
  hSet(key: string, field: string, value: any): void;
  hGetAll(key: string): Record<string, string>;
  hDel(key: string, ...fields: string[]): number;
  sAdd(key: string, ...members: any[]): number;
  sRem(key: string, ...members: any[]): number;
  sMembers(key: string): string[];
  sIsMember(key: string, member: any): boolean;
  lPush(key: string, ...values: any[]): number;
  rPush(key: string, ...values: any[]): number;
  lRange(key: string, start: number, stop: number): string[];
  lLen(key: string): number;
  zAdd(key: string, score: number, member: string): boolean;
  zRem(key: string, ...members: any[]): number;
  zRange(key: string, start: number, stop: number): string[];
  zScore(key: string, member: string): number | null;
  incr(key: string): number;
  incrBy(key: string, delta: number): number;
  // Any Redis command verbatim, e.g. command('SETBIT', 'flags', 7, 1).
  command(...args: any[]): any;
}

// The root handle passed to up()/down(). In this single-database project it is
// bound directly to the redis database; use(alias) also works.
type Handle = DbHandle & {
  use: (alias: string) => DbHandle;
}

type Console = {
  log(...args: any[]): void;
}
declare const console: Console;
