CREATE TABLE IF NOT EXISTS {{.TableName}} (
    filename TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    applied_at TEXT NOT NULL
);
