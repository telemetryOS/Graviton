CREATE TABLE IF NOT EXISTS {{.TableName}} (
    filename VARCHAR(255) PRIMARY KEY,
    source TEXT NOT NULL,
    applied_at DATETIME NOT NULL
);
