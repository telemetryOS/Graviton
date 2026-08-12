SELECT holder, hostname, pid, acquired_at
FROM {{.LockTableName}}
WHERE id = 1;
