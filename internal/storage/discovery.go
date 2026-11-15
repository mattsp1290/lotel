package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type DiscoveryOptions struct {
	Service string
	Since   time.Time
	Until   time.Time
	Limit   int
}

func (opts DiscoveryOptions) queryOptions() QueryOptions {
	return QueryOptions{Service: opts.Service, Since: opts.Since, Until: opts.Until, Limit: opts.Limit}
}

type ServiceResult struct {
	ServiceName string   `json:"service_name"`
	Signals     []string `json:"signals"`
}

type OperationResult struct {
	ServiceName string `json:"service_name"`
	Name        string `json:"name"`
	Kind        int    `json:"kind"`
}

func QueryServices(db *sql.DB, opts DiscoveryOptions) ([]ServiceResult, error) {
	var parts []string
	var args []any
	for _, source := range []struct{ table, time, signal string }{
		{"traces", "start_time", "traces"},
		{"metrics", "timestamp", "metrics"},
		{"logs", "timestamp", "logs"},
	} {
		part := fmt.Sprintf("SELECT service_name, '%s' AS signal FROM %s WHERE 1=1", source.signal, source.table)
		args = append(args, buildWhere(&part, opts.queryOptions(), source.time)...)
		parts = append(parts, part)
	}
	query := "SELECT service_name, CAST(to_json(list(DISTINCT signal ORDER BY signal)) AS VARCHAR) FROM (" + strings.Join(parts, " UNION ALL ") + ") observed GROUP BY service_name ORDER BY service_name"
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying services: %w", err)
	}
	defer rows.Close()
	results := []ServiceResult{}
	for rows.Next() {
		var result ServiceResult
		var signals string
		if err := rows.Scan(&result.ServiceName, &signals); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(signals), &result.Signals); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

func QueryOperations(db *sql.DB, opts DiscoveryOptions) ([]OperationResult, error) {
	query := "SELECT DISTINCT service_name, name, kind FROM traces WHERE 1=1"
	args := buildWhere(&query, opts.queryOptions(), "start_time")
	query += " ORDER BY service_name, name, kind"
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying operations: %w", err)
	}
	defer rows.Close()
	results := []OperationResult{}
	for rows.Next() {
		var result OperationResult
		if err := rows.Scan(&result.ServiceName, &result.Name, &result.Kind); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
}
