package storage

import (
	"database/sql"
	"fmt"
	"time"

	duckdb "github.com/marcboeker/go-duckdb"
)

type DiscoveryOptions struct {
	Service string
	Since   time.Time
	Until   time.Time
	Limit   int
}

func (opts DiscoveryOptions) Validate() error {
	if opts.Limit < 0 {
		return fmt.Errorf("limit cannot be negative")
	}
	if !opts.Since.IsZero() && !opts.Until.IsZero() && opts.Since.After(opts.Until) {
		return fmt.Errorf("since must not be after until")
	}
	return nil
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
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	query := `SELECT service_name, list(DISTINCT signal ORDER BY signal) FROM (
		SELECT service_name, start_time AS observed_at, 'traces' AS signal FROM traces
		UNION ALL SELECT service_name, timestamp AS observed_at, 'metrics' AS signal FROM metrics
		UNION ALL SELECT service_name, timestamp AS observed_at, 'logs' AS signal FROM logs
	) observed WHERE 1=1`
	args := buildWhere(&query, opts.queryOptions(), "observed_at")
	query += " GROUP BY service_name ORDER BY service_name"
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
		var signals duckdb.Composite[[]string]
		if err := rows.Scan(&result.ServiceName, &signals); err != nil {
			return nil, err
		}
		result.Signals = signals.Get()
		results = append(results, result)
	}
	return results, rows.Err()
}

func QueryOperations(db *sql.DB, opts DiscoveryOptions) ([]OperationResult, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
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
