package storage

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

type DependencyOptions struct {
	Since time.Time
	Until time.Time
}

type DependencyEdge struct {
	SourceService string `json:"source_service"`
	TargetService string `json:"target_service"`
	ObservedCalls int64  `json:"observed_calls"`
}

type DependencyExclusions struct {
	InvalidChildID  int64 `json:"invalid_child_id"`
	DuplicateChild  int64 `json:"duplicate_child"`
	InvalidParentID int64 `json:"invalid_parent_id"`
	MissingParent   int64 `json:"missing_parent"`
	DuplicateParent int64 `json:"duplicate_parent"`
	UnknownService  int64 `json:"unknown_service"`
	NonClientParent int64 `json:"non_client_parent"`
	SameService     int64 `json:"same_service"`
}

type DependencyReport struct {
	Edges      []DependencyEdge     `json:"edges"`
	Exclusions DependencyExclusions `json:"exclusions"`
}

func validDependencyID(id string, bytes int) bool {
	decoded, err := hex.DecodeString(id)
	return err == nil && len(decoded) == bytes && strings.Trim(id, "0") != ""
}

func QueryDependencies(db *sql.DB, opts DependencyOptions) (DependencyReport, error) {
	report := DependencyReport{Edges: []DependencyEdge{}}
	if opts.Since.IsZero() || opts.Until.IsZero() || opts.Since.After(opts.Until) {
		return report, fmt.Errorf("invalid dependency window")
	}
	start, end := projectedWindow(opts.Since, opts.Until)
	rows, err := db.Query(`WITH identities AS (
		SELECT lower(trace_id) AS trace_key, lower(span_id) AS span_key, count(*) AS n
		FROM traces GROUP BY trace_key, span_key
	), unique_spans AS (
		SELECT t.* FROM traces t JOIN identities i
		ON lower(t.trace_id) = i.trace_key AND lower(t.span_id) = i.span_key
		WHERE i.n = 1
	)
	SELECT c.trace_id, c.span_id, c.parent_span_id, c.service_name, ci.n,
		coalesce(pi.n, 0), p.kind, p.service_name
	FROM traces c
	JOIN identities ci ON lower(c.trace_id) = ci.trace_key AND lower(c.span_id) = ci.span_key
	LEFT JOIN identities pi ON lower(c.trace_id) = pi.trace_key AND lower(c.parent_span_id) = pi.span_key
	LEFT JOIN unique_spans p ON lower(c.trace_id) = lower(p.trace_id) AND lower(c.parent_span_id) = lower(p.span_id)
	WHERE c.kind = 2 AND c.start_time >= ? AND c.start_time <= ?`, start, end)
	if err != nil {
		return report, fmt.Errorf("querying dependencies: %w", err)
	}
	defer rows.Close()
	type edgeKey struct{ source, target string }
	counts := map[edgeKey]int64{}
	for rows.Next() {
		var trace, child, target string
		var parent, source sql.NullString
		var childCount, parentCount int64
		var parentKind sql.NullInt64
		if err := rows.Scan(&trace, &child, &parent, &target, &childCount, &parentCount, &parentKind, &source); err != nil {
			return report, fmt.Errorf("scanning dependencies: %w", err)
		}
		switch {
		case !validDependencyID(trace, 16) || !validDependencyID(child, 8):
			report.Exclusions.InvalidChildID++
		case childCount != 1:
			report.Exclusions.DuplicateChild++
		case !parent.Valid || parent.String == "":
			report.Exclusions.MissingParent++
		case !validDependencyID(parent.String, 8):
			report.Exclusions.InvalidParentID++
		case parentCount == 0:
			report.Exclusions.MissingParent++
		case parentCount != 1:
			report.Exclusions.DuplicateParent++
		case target == "" || target == "unknown" || !source.Valid || source.String == "" || source.String == "unknown":
			report.Exclusions.UnknownService++
		case !parentKind.Valid || parentKind.Int64 != 3:
			report.Exclusions.NonClientParent++
		case source.String == target:
			report.Exclusions.SameService++
		default:
			counts[edgeKey{source.String, target}]++
		}
	}
	if err := rows.Err(); err != nil {
		return report, err
	}
	for key, count := range counts {
		report.Edges = append(report.Edges, DependencyEdge{key.source, key.target, count})
	}
	sort.Slice(report.Edges, func(i, j int) bool {
		if report.Edges[i].SourceService != report.Edges[j].SourceService {
			return report.Edges[i].SourceService < report.Edges[j].SourceService
		}
		return report.Edges[i].TargetService < report.Edges[j].TargetService
	})
	return report, nil
}
