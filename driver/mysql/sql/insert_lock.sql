INSERT INTO {{.LockTableName}} (id, holder, hostname, pid, acquired_at)
SELECT 1, ?, ?, ?, ? FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM {{.LockTableName}} AS held);
