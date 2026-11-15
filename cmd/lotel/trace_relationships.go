package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
)

type traceReference struct {
	State       string `json:"state"`
	SpanIndices []int  `json:"span_indices,omitempty"`
}

type traceRelationships struct {
	SpanParents      []traceReference `json:"span_parents"`
	LogSpans         []traceReference `json:"log_spans"`
	CycleSpanIndices []int            `json:"cycle_span_indices"`
}

type sortableRecord[T any] struct {
	value T
	time  time.Time
	json  string
}

func sortTraceRecords[T any](records []T, timestamp func(T) time.Time) error {
	sorted := make([]sortableRecord[T], len(records))
	for i, record := range records {
		encoded, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("encoding trace record for ordering: %w", err)
		}
		sorted[i] = sortableRecord[T]{value: record, time: timestamp(record), json: string(encoded)}
	}
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].time.Equal(sorted[j].time) {
			return sorted[i].time.Before(sorted[j].time)
		}
		return sorted[i].json < sorted[j].json
	})
	for i, record := range sorted {
		records[i] = record.value
	}
	return nil
}

func normalizedSpanID(id string) (string, bool) {
	decoded, err := hex.DecodeString(id)
	if err != nil || len(decoded) != 8 || strings.Trim(id, "0") == "" {
		return "", false
	}
	return strings.ToLower(id), true
}

func resolveTraceReference(id, emptyState string, index map[string][]int) traceReference {
	if id == "" {
		return traceReference{State: emptyState}
	}
	id, valid := normalizedSpanID(id)
	if !valid {
		return traceReference{State: "invalid"}
	}
	candidates := index[id]
	switch len(candidates) {
	case 0:
		return traceReference{State: "missing"}
	case 1:
		return traceReference{State: "linked", SpanIndices: candidates}
	default:
		return traceReference{State: "ambiguous", SpanIndices: candidates}
	}
}

func cycleSpanIndices(parents []traceReference) []int {
	state := make([]uint8, len(parents))
	inCycle := make([]bool, len(parents))
	for start := range parents {
		if state[start] != 0 {
			continue
		}
		path := []int{}
		positions := make(map[int]int)
		current := start
		for state[current] == 0 {
			state[current] = 1
			positions[current] = len(path)
			path = append(path, current)
			if parents[current].State != "linked" {
				current = -1
				break
			}
			current = parents[current].SpanIndices[0]
		}
		if current >= 0 && state[current] == 1 {
			if position, ok := positions[current]; ok {
				for _, member := range path[position:] {
					inCycle[member] = true
				}
			}
		}
		for _, member := range path {
			state[member] = 2
		}
	}
	result := []int{}
	for i, member := range inCycle {
		if member {
			result = append(result, i)
		}
	}
	return result
}

func (bundle *traceBundle) resolveRelationships() error {
	if err := sortTraceRecords(bundle.Spans, func(span storage.TraceResult) time.Time { return span.StartTime }); err != nil {
		return err
	}
	if err := sortTraceRecords(bundle.Logs, func(log storage.LogResult) time.Time { return log.Timestamp }); err != nil {
		return err
	}
	index := make(map[string][]int)
	for i, span := range bundle.Spans {
		if id, ok := normalizedSpanID(span.SpanID); ok {
			index[id] = append(index[id], i)
		}
	}
	relationships := &traceRelationships{
		SpanParents: make([]traceReference, len(bundle.Spans)),
		LogSpans:    make([]traceReference, len(bundle.Logs)),
	}
	for i, span := range bundle.Spans {
		relationships.SpanParents[i] = resolveTraceReference(span.ParentSpanID, "root", index)
	}
	for i, log := range bundle.Logs {
		relationships.LogSpans[i] = resolveTraceReference(log.SpanID, "unscoped", index)
	}
	relationships.CycleSpanIndices = cycleSpanIndices(relationships.SpanParents)
	bundle.Relationships = relationships
	return nil
}
