package storage

import (
	"fmt"
)

const MaxRecentLimit = 1000

func buildRecentWhere(query *string, opts QueryOptions, timeColumn string) []any {
	*query += fmt.Sprintf(" AND %s >= ? AND %s <= ?", timeColumn, timeColumn)
	args := []any{opts.Since, opts.Until}
	if opts.Service != "" {
		*query += " AND service_name = ?"
		args = append(args, opts.Service)
	}
	return args
}

func recentOptions(opts QueryOptions) (QueryOptions, error) {
	if opts.Since.IsZero() || opts.Until.IsZero() || opts.Since.After(opts.Until) || opts.Limit < 1 || opts.Limit > MaxRecentLimit {
		return opts, fmt.Errorf("invalid recent query window or limit")
	}
	opts.Since, opts.Until = projectedWindow(opts.Since, opts.Until)
	opts.Limit++
	return opts, nil
}

func RecentTraces(db queryer, opts TraceQueryOptions) ([]TraceResult, bool, error) {
	limit := opts.Limit
	var err error
	opts.QueryOptions, err = recentOptions(opts.QueryOptions)
	if err != nil {
		return nil, false, err
	}
	rows, err := queryTraces(db, opts, true)
	if err != nil {
		return nil, false, err
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	if rows == nil {
		rows = []TraceResult{}
	}
	return rows, truncated, nil
}

func RecentLogs(db queryer, opts LogQueryOptions) ([]LogResult, bool, error) {
	if opts.Name != "" {
		return nil, false, fmt.Errorf("name filter is not supported for recent logs")
	}
	limit := opts.Limit
	var err error
	opts.QueryOptions, err = recentOptions(opts.QueryOptions)
	if err != nil {
		return nil, false, err
	}
	rows, err := queryLogs(db, opts, true)
	if err != nil {
		return nil, false, err
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	if rows == nil {
		rows = []LogResult{}
	}
	return rows, truncated, nil
}
