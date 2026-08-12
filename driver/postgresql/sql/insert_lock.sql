INSERT INTO {{.LockTableName}} (id, holder, hostname, pid, acquired_at)
SELECT 1, $1, $2, $3, $4
WHERE NOT EXISTS (SELECT 1 FROM {{.LockTableName}});
