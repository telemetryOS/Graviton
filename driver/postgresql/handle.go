package postgresql

import (
	"context"
	"database/sql"
)

type Handle struct {
	ctx    context.Context
	driver *Driver
}

type SQLResult struct {
	RowsAffected int64
}

func (h *Handle) Exec(sqlQuery *SQLQuery) *SQLResult {
	execer := h.getExecutor()

	result, err := execer.ExecContext(h.ctx, sqlQuery.Query, sqlQuery.Params...)
	if err != nil {
		panic(err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		panic(err)
	}

	return &SQLResult{
		RowsAffected: rowsAffected,
	}
}

func (h *Handle) Query(sqlQuery *SQLQuery) []map[string]any {
	execer := h.getExecutor()

	var rows *sql.Rows
	var err error

	switch ex := execer.(type) {
	case *sql.DB:
		rows, err = ex.QueryContext(h.ctx, sqlQuery.Query, sqlQuery.Params...)
	case *sql.Tx:
		rows, err = ex.QueryContext(h.ctx, sqlQuery.Query, sqlQuery.Params...)
	}

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

func (h *Handle) getExecutor() interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
} {
	if tx := h.driver.getTxFromContext(h.ctx); tx != nil {
		return tx
	}
	return h.driver.db
}
