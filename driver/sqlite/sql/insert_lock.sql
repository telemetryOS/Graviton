INSERT INTO {{.LockTableName}} (id, holder, hostname, pid, acquired_at)
SELECT 1, ?, ?, ?, ?
WHERE NOT EXISTS (SELECT 1 FROM {{.LockTableName}});
