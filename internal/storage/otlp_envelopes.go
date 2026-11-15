package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

type otlpSignal struct{ name, resourceKey, scopeKey, recordKey string }

var otlpSignals = []otlpSignal{
	{"traces", "resourceSpans", "scopeSpans", "spans"},
	{"metrics", "resourceMetrics", "scopeMetrics", "metrics"},
	{"logs", "resourceLogs", "scopeLogs", "logRecords"},
	{"profiles", "resourceProfiles", "scopeProfiles", "profiles"},
}

type OTLPResult struct {
	Signal            string          `json:"signal"`
	ServiceName       string          `json:"service_name"`
	FirstTimeUnixNano string          `json:"first_time_unix_nano,omitempty"`
	LastTimeUnixNano  string          `json:"last_time_unix_nano,omitempty"`
	Payload           json.RawMessage `json:"payload"`
}

func otlpSignalFor(name string) (otlpSignal, error) {
	for _, signal := range otlpSignals {
		if signal.name == name {
			return signal, nil
		}
	}
	return otlpSignal{}, fmt.Errorf("unsupported OTLP signal %q", name)
}

func splitOTLPEnvelopes(name string, line []byte) ([]OTLPResult, error) {
	signal, err := otlpSignalFor(name)
	if err != nil {
		return nil, err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(line, &root); err != nil {
		return nil, err
	}
	resourcesJSON, ok := root[signal.resourceKey]
	if !ok {
		if root != nil && (len(root) == 0 || name == "profiles" && len(root) == 1 && root["dictionary"] != nil) {
			return []OTLPResult{{Signal: name, ServiceName: "unknown", Payload: append(json.RawMessage(nil), line...)}}, nil
		}
		return nil, fmt.Errorf("missing %s", signal.resourceKey)
	}
	var resources []json.RawMessage
	if err := json.Unmarshal(resourcesJSON, &resources); err != nil {
		return nil, err
	}
	if len(resources) == 0 {
		return []OTLPResult{{Signal: name, ServiceName: "unknown", Payload: append(json.RawMessage(nil), line...)}}, nil
	}
	result := make([]OTLPResult, 0, len(resources))
	for _, resource := range resources {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(resource, &fields); err != nil || fields == nil {
			return nil, fmt.Errorf("invalid resource envelope")
		}
		service := rawServiceName(fields["resource"], root["dictionary"])
		root[signal.resourceKey] = json.RawMessage("[" + string(resource) + "]")
		payload, err := json.Marshal(root)
		if err != nil {
			return nil, err
		}
		first, last := rawTimeRange(fields[signal.scopeKey])
		result = append(result, OTLPResult{Signal: name, ServiceName: service, FirstTimeUnixNano: first, LastTimeUnixNano: last, Payload: payload})
	}
	return result, nil
}

func rawServiceName(resource, dictionary json.RawMessage) string {
	var table struct {
		StringTable []string `json:"stringTable"`
	}
	if dictionary != nil {
		_ = json.Unmarshal(dictionary, &table)
	}
	resolve := func(direct *string, index *int) (string, bool) {
		if direct != nil {
			return *direct, true
		}
		if index != nil && *index >= 0 && *index < len(table.StringTable) {
			return table.StringTable[*index], true
		}
		return "", false
	}
	var value struct {
		Attributes []struct {
			Key         *string `json:"key"`
			KeyStrindex *int    `json:"keyStrindex"`
			Value       struct {
				StringValue         *string `json:"stringValue"`
				StringValueStrindex *int    `json:"stringValueStrindex"`
			} `json:"value"`
		} `json:"attributes"`
	}
	if json.Unmarshal(resource, &value) == nil {
		for _, attr := range value.Attributes {
			key, _ := resolve(attr.Key, attr.KeyStrindex)
			if key == "service.name" {
				if service, ok := resolve(attr.Value.StringValue, attr.Value.StringValueStrindex); ok {
					return service
				}
			}
		}
	}
	return "unknown"
}

func rawNano(value json.RawMessage) (uint64, bool) {
	var text string
	if len(value) == 0 {
		return 0, false
	}
	if value[0] == '"' {
		if json.Unmarshal(value, &text) != nil {
			return 0, false
		}
	} else {
		text = string(value)
	}
	n, err := strconv.ParseUint(text, 10, 64)
	return n, err == nil && n != 0
}

func rawTimeRange(value json.RawMessage) (string, string) {
	var first, last uint64
	add := func(n uint64) {
		if first == 0 || n < first {
			first = n
		}
		if n > last {
			last = n
		}
	}
	var walk func(json.RawMessage)
	walk = func(value json.RawMessage) {
		if len(value) == 0 {
			return
		}
		switch value[0] {
		case '[':
			var items []json.RawMessage
			if json.Unmarshal(value, &items) == nil {
				for _, item := range items {
					walk(item)
				}
			}
		case '{':
			var fields map[string]json.RawMessage
			if json.Unmarshal(value, &fields) != nil {
				return
			}
			for key, child := range fields {
				switch key {
				case "timeUnixNano", "startTimeUnixNano", "endTimeUnixNano", "observedTimeUnixNano":
					if n, ok := rawNano(child); ok {
						add(n)
					}
				case "timestampsUnixNano":
					var timestamps []json.RawMessage
					if json.Unmarshal(child, &timestamps) == nil {
						for _, timestamp := range timestamps {
							if n, ok := rawNano(timestamp); ok {
								add(n)
							}
						}
					}
				default:
					walk(child)
				}
			}
			if start, ok := rawNano(fields["timeUnixNano"]); ok {
				if duration, ok := rawNano(fields["durationNano"]); ok && duration <= ^uint64(0)-start {
					add(start + duration)
				}
			}
		}
	}
	walk(value)
	if first == 0 {
		return "", ""
	}
	return strconv.FormatUint(first, 10), strconv.FormatUint(last, 10)
}

func nullableNano(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func queryNano(t time.Time) (string, error) {
	if t.Before(time.Unix(0, 0)) {
		return "", fmt.Errorf("OTLP time filter precedes the Unix epoch")
	}
	seconds := uint64(t.Unix())
	if seconds > (^uint64(0)-uint64(t.Nanosecond()))/1_000_000_000 {
		return "", fmt.Errorf("OTLP time filter exceeds uint64 nanoseconds")
	}
	return strconv.FormatUint(seconds*1_000_000_000+uint64(t.Nanosecond()), 10), nil
}

func QueryOTLP(db *sql.DB, signal string, opts QueryOptions) ([]OTLPResult, error) {
	if _, err := otlpSignalFor(signal); err != nil {
		return nil, err
	}
	if opts.Name != "" || opts.TraceID != "" || opts.EventName != "" || len(opts.Attrs) != 0 {
		return nil, fmt.Errorf("OTLP queries support only service, time, and limit filters")
	}
	if opts.Limit < 0 {
		return nil, fmt.Errorf("limit cannot be negative")
	}
	if !opts.Since.IsZero() && !opts.Until.IsZero() && opts.Since.After(opts.Until) {
		return nil, fmt.Errorf("since must not be after until")
	}
	query := `SELECT signal,service_name,CAST(first_nano AS VARCHAR),CAST(last_nano AS VARCHAR),CAST(payload AS VARCHAR) FROM otlp_envelopes WHERE signal=?`
	args := []any{signal}
	if opts.Service != "" {
		query += " AND service_name=?"
		args = append(args, opts.Service)
	}
	for _, bound := range []struct {
		value  time.Time
		clause string
	}{{opts.Since, " AND last_nano>=CAST(? AS UBIGINT)"}, {opts.Until, " AND first_nano<=CAST(? AS UBIGINT)"}} {
		if !bound.value.IsZero() {
			n, err := queryNano(bound.value)
			if err != nil {
				return nil, err
			}
			query += bound.clause
			args = append(args, n)
		}
	}
	query += " ORDER BY first_nano ASC NULLS LAST,rowid ASC"
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying OTLP envelopes: %w", err)
	}
	defer rows.Close()
	result := []OTLPResult{}
	for rows.Next() {
		var item OTLPResult
		var first, last sql.NullString
		var payload string
		if err := rows.Scan(&item.Signal, &item.ServiceName, &first, &last, &payload); err != nil {
			return nil, err
		}
		item.FirstTimeUnixNano, item.LastTimeUnixNano, item.Payload = first.String, last.String, json.RawMessage(payload)
		result = append(result, item)
	}
	return result, rows.Err()
}

func countProfileRecords(envelopes []OTLPResult) int {
	count := 0
	for _, envelope := range envelopes {
		var root struct {
			ResourceProfiles []struct {
				ScopeProfiles []struct {
					Profiles []json.RawMessage `json:"profiles"`
				} `json:"scopeProfiles"`
			} `json:"resourceProfiles"`
		}
		if json.Unmarshal(envelope.Payload, &root) == nil {
			for _, resource := range root.ResourceProfiles {
				for _, scope := range resource.ScopeProfiles {
					count += len(scope.Profiles)
				}
			}
		}
	}
	return count
}
