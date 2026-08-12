DELETE FROM {{.LockTableName}}
WHERE id = 1 AND holder = ?;
