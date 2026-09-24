package sqlite

import (
	"context"

	"github.com/telemetryos/graviton/driver/transaction"
)

type Handle struct {
	ctx    context.Context
	driver *Driver
}

type SQLResult struct {
	RowsAffected int64
	LastInsertId int64
}

func (h *Handle) Exec(sqlQuery *SQLQuery) *SQLResult {
	execer := h.executor()

	result, err := execer.ExecContext(h.ctx, sqlQuery.Query, sqlQuery.Params...)
	if err != nil {
		panic(err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		panic(err)
	}

	lastInsertId, _ := result.LastInsertId()

	return &SQLResult{
		RowsAffected: rowsAffected,
		LastInsertId: lastInsertId,
	}
}

func (h *Handle) Query(sqlQuery *SQLQuery) []map[string]any {
	execer := h.executor()

	rows, err := execer.QueryContext(h.ctx, sqlQuery.Query, sqlQuery.Params...)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		panic(err)
	}

	var results []map[string]any
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			panic(err)
		}

		row := make(map[string]any)
		for i, col := range columns {
			row[col] = values[i]
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		panic(err)
	}

	return results
}

func (h *Handle) QueryOne(sqlQuery *SQLQuery) map[string]any {
	results := h.Query(sqlQuery)
	if len(results) == 0 {
		return nil
	}
	return results[0]
}

// executor selects the connection associated with this handle.
func (h *Handle) executor() sqlExecutor {
	if transaction.Bound(h.ctx) {
		return h.driver.tx
	}
	return h.driver.db
}
