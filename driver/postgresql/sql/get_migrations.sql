SELECT filename, source, applied_at
FROM {{.TableName}}
ORDER BY filename;
