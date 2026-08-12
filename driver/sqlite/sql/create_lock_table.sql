CREATE TABLE IF NOT EXISTS {{.LockTableName}} (
    id INTEGER PRIMARY KEY,
    holder TEXT NOT NULL,
    hostname TEXT NOT NULL,
    pid INTEGER NOT NULL,
    acquired_at TEXT NOT NULL
);
