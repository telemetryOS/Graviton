type SQLQuery = {
  query: string;
  params: any[];
  validated: boolean;
}

type SQLResult = {
  rowsAffected: number;
  lastInsertId: number;
}

type Handle = {
  exec(query: SQLQuery): SQLResult;
  query<T = any>(query: SQLQuery): T[];
  queryOne<T = any>(query: SQLQuery): T | null;
}

declare function sql(strings: TemplateStringsArray, ...values: any[]): SQLQuery;

type Console = {
  log(...args: any[]): void;
}
declare const console: Console;
