package storage

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"time"
)

type SignalEvidence struct {
	Signal           string     `json:"signal"`
	ObservedInWindow *bool      `json:"observed_in_window"`
	LatestInWindow   *time.Time `json:"latest_in_window"`
}

type SignalDiagnosis struct {
	SchemaVersion int              `json:"schema_version"`
	Observation   string           `json:"observation"`
	Reason        string           `json:"reason,omitempty"`
	Service       string           `json:"service"`
	Since         time.Time        `json:"since"`
	Until         time.Time        `json:"until"`
	Signals       []SignalEvidence `json:"signals"`
}

func unavailableSignals(service string, since, until time.Time, reason string) SignalDiagnosis {
	return SignalDiagnosis{SchemaVersion: 1, Observation: "unavailable", Reason: reason, Service: service, Since: since, Until: until, Signals: []SignalEvidence{{Signal: "traces"}, {Signal: "metrics"}, {Signal: "logs"}}}
}

func unavailableReason(err error) string {
	if errors.Is(err, os.ErrNotExist) {
		return "data_missing"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "data_busy"
	}
	return "data_unavailable"
}

func DiagnoseSignals(ctx context.Context, path, service string, since, until time.Time) (report SignalDiagnosis) {
	report = unavailableSignals(service, since, until, "data_unavailable")
	if service == "" || since.IsZero() || until.IsZero() || since.After(until) {
		return
	}
	if strings.ContainsAny(path, "?#%\x00") {
		report.Reason = "unsafe_path"
		return
	}
	dsn := path + "?access_mode=read_only&temp_directory=&enable_external_access=false&autoinstall_known_extensions=false&autoload_known_extensions=false"
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.RawQuery != "access_mode=read_only&temp_directory=&enable_external_access=false&autoinstall_known_extensions=false&autoload_known_extensions=false" {
		report.Reason = "unsafe_path"
		return
	}
	lockPath := path + ".lock"
	lock, err := openExistingDiagnosticFile(lockPath)
	if err != nil {
		report.Reason = unavailableReason(err)
		return
	}
	defer lock.Close()
	lockInfo, err := lock.Stat()
	if err != nil || !lockInfo.Mode().IsRegular() {
		return
	}
	for {
		acquired, e := tryDatabaseLock(lock)
		if e != nil {
			return
		}
		if acquired {
			break
		}
		select {
		case <-ctx.Done():
			report.Reason = "data_busy"
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer releaseDatabaseLock(lock)
	before, err := os.Lstat(path)
	if err != nil {
		report.Reason = unavailableReason(err)
		return
	}
	if !before.Mode().IsRegular() {
		return
	}
	pathLock, err := os.Lstat(lockPath)
	if err != nil || !os.SameFile(lockInfo, pathLock) {
		return
	}
	db, err := sql.Open("duckdb", dsn)
	if err != nil {
		return
	}
	closed := false
	defer func() {
		if !closed {
			_ = db.Close()
		}
	}()
	var mode, tempDir, external, install, autoload string
	err = db.QueryRowContext(ctx, `SELECT current_setting('access_mode'), current_setting('temp_directory'), current_setting('enable_external_access'), current_setting('autoinstall_known_extensions'), current_setting('autoload_known_extensions')`).Scan(&mode, &tempDir, &external, &install, &autoload)
	if err != nil || mode != "read_only" || tempDir != "" || external != "false" || install != "false" || autoload != "false" {
		return
	}
	var schema string
	if err = db.QueryRowContext(ctx, `SELECT value FROM schema_meta WHERE key='schema_version'`).Scan(&schema); err != nil || schema != "3" {
		report.Reason = "schema_unavailable"
		return
	}
	start, end := projectedWindow(since, until)
	query := `SELECT 'traces', MAX(start_time) FROM traces WHERE service_name=? AND start_time>=? AND start_time<=? UNION ALL SELECT 'metrics', MAX(timestamp) FROM metrics WHERE service_name=? AND timestamp>=? AND timestamp<=? UNION ALL SELECT 'logs', MAX(timestamp) FROM logs WHERE service_name=? AND timestamp>=? AND timestamp<=?`
	rows, err := db.QueryContext(ctx, query, service, start, end, service, start, end, service, start, end)
	if err != nil {
		return
	}
	results := make(map[string]*time.Time, 3)
	for rows.Next() {
		var signal string
		var stamp sql.NullTime
		if err = rows.Scan(&signal, &stamp); err != nil {
			break
		}
		if stamp.Valid {
			t := stamp.Time.UTC()
			results[signal] = &t
		} else {
			results[signal] = nil
		}
	}
	if err == nil {
		err = rows.Err()
	}
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	if err != nil || len(results) != 3 {
		return
	}
	if err = db.Close(); err != nil {
		return
	}
	closed = true
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) {
		return
	}
	pathLock, err = os.Lstat(lockPath)
	if err != nil || !os.SameFile(lockInfo, pathLock) {
		return
	}
	report.Observation = "available"
	report.Reason = ""
	for i := range report.Signals {
		latest := results[report.Signals[i].Signal]
		observed := latest != nil
		report.Signals[i].ObservedInWindow = &observed
		report.Signals[i].LatestInWindow = latest
	}
	return
}
