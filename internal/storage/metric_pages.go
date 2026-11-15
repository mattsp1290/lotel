package storage

import (
	"database/sql"
	"fmt"
	"strings"
)

const metricPageByteLimit = 4 << 20
const metricPageRowLimit = 64

type metricEnvelopeRef struct {
	id   int64
	size int
}

func metricEnvelopePages(tx *sql.Tx) ([][]metricEnvelopeRef, error) {
	rows, err := tx.Query(`SELECT rowid, strlen(CAST(payload AS VARCHAR)) FROM otlp_envelopes WHERE signal='metrics' ORDER BY rowid LIMIT ?`, metricEnvelopeLimit+1)
	if err != nil {
		return nil, fmt.Errorf("querying metric envelope metadata: %w", err)
	}
	defer rows.Close()
	var pages [][]metricEnvelopeRef
	var count, total, pageBytes int
	for rows.Next() {
		var ref metricEnvelopeRef
		if err := rows.Scan(&ref.id, &ref.size); err != nil {
			return nil, err
		}
		count++
		if count > metricEnvelopeLimit {
			return nil, fmt.Errorf("indeterminate: metric envelope scan limit exceeded")
		}
		if ref.size < 0 || ref.size > metricEnvelopeByteLimit {
			return nil, fmt.Errorf("indeterminate: metric envelope size limit exceeded")
		}
		total += ref.size
		if total > metricTotalByteLimit {
			return nil, fmt.Errorf("indeterminate: metric scan byte limit exceeded")
		}
		if len(pages) == 0 || len(pages[len(pages)-1]) >= metricPageRowLimit || pageBytes+ref.size > metricPageByteLimit {
			pages = append(pages, []metricEnvelopeRef{})
			pageBytes = 0
		}
		pages[len(pages)-1] = append(pages[len(pages)-1], ref)
		pageBytes += ref.size
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return pages, nil
}

func visitMetricPage(tx *sql.Tx, refs []metricEnvelopeRef, visit func([]byte) error) error {
	placeholders := make([]string, len(refs))
	args := make([]any, len(refs))
	sizes := make(map[int64]int, len(refs))
	for i, ref := range refs {
		placeholders[i] = "?"
		args[i] = ref.id
		sizes[ref.id] = ref.size
	}
	rows, err := tx.Query(`SELECT rowid, CAST(payload AS VARCHAR) FROM otlp_envelopes WHERE signal='metrics' AND rowid IN (`+strings.Join(placeholders, ",")+`) ORDER BY rowid`, args...)
	if err != nil {
		return fmt.Errorf("querying metric envelope page: %w", err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return err
		}
		expected, ok := sizes[id]
		if !ok || len(raw) != expected {
			return fmt.Errorf("indeterminate: metric envelope changed during scan")
		}
		delete(sizes, id)
		seen++
		if err := visit([]byte(raw)); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if seen != len(refs) {
		return fmt.Errorf("indeterminate: metric envelope missing during scan")
	}
	return nil
}
