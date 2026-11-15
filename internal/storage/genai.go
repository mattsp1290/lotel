package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

type GenAIOptions struct {
	QueryOptions
	Model, Provider, Operation, ConversationID string
	ErrorsOnly, Content                        bool
	GroupBy                                    []string
}

type GenAICall struct {
	TraceID               string    `json:"trace_id"`
	SpanID                string    `json:"span_id"`
	ParentSpanID          string    `json:"parent_span_id,omitempty"`
	SpanName              string    `json:"span_name"`
	ServiceName           string    `json:"service_name"`
	StartTime             time.Time `json:"start_time"`
	DurationMS            float64   `json:"duration_ms"`
	StatusCode            int       `json:"status_code"`
	EventCount            int       `json:"event_count"`
	Operation             string    `json:"operation"`
	Provider              string    `json:"provider"`
	RequestModel          string    `json:"request_model"`
	ResponseModel         string    `json:"response_model,omitempty"`
	ResponseID            string    `json:"response_id,omitempty"`
	FinishReasons         []string  `json:"finish_reasons,omitempty"`
	ConversationID        string    `json:"conversation_id,omitempty"`
	AgentName             string    `json:"agent_name,omitempty"`
	ToolName              string    `json:"tool_name,omitempty"`
	InputTokens           *int64    `json:"input_tokens"`
	OutputTokens          *int64    `json:"output_tokens"`
	CacheReadInputTokens  *int64    `json:"cache_read_input_tokens"`
	CacheWriteInputTokens *int64    `json:"cache_write_input_tokens"`
	ReasoningOutputTokens *int64    `json:"reasoning_output_tokens"`
	ErrorType             string    `json:"error_type,omitempty"`
	InputMessages         any       `json:"input_messages,omitempty"`
	OutputMessages        any       `json:"output_messages,omitempty"`
	SystemInstructions    any       `json:"system_instructions,omitempty"`
	ToolDefinitions       any       `json:"tool_definitions,omitempty"`
	ToolCallArguments     any       `json:"tool_call_arguments,omitempty"`
	ToolCallResult        any       `json:"tool_call_result,omitempty"`
	ContentSource         string    `json:"content_source,omitempty"`
}

var genAIAliases = []struct {
	field string
	keys  []string
}{
	{"operation", []string{"gen_ai.operation.name"}},
	{"provider", []string{"gen_ai.provider.name", "gen_ai.system"}},
	{"request_model", []string{"gen_ai.request.model"}},
	{"response_model", []string{"gen_ai.response.model"}},
	{"response_id", []string{"gen_ai.response.id"}},
	{"conversation_id", []string{"gen_ai.conversation.id"}},
	{"agent_name", []string{"gen_ai.agent.name"}},
	{"tool_name", []string{"gen_ai.tool.name"}},
	{"input_tokens", []string{"gen_ai.usage.input_tokens", "gen_ai.usage.prompt_tokens"}},
	{"output_tokens", []string{"gen_ai.usage.output_tokens", "gen_ai.usage.completion_tokens"}},
	{"cache_read_input_tokens", []string{"gen_ai.usage.cache_read.input_tokens"}},
	{"cache_write_input_tokens", []string{"gen_ai.usage.cache_write.input_tokens", "gen_ai.usage.cache_creation.input_tokens"}},
	{"reasoning_output_tokens", []string{"gen_ai.usage.reasoning.output_tokens"}},
}

func aliasExpression(keys []string) string {
	parts := make([]string, 0, len(keys)+1)
	for _, key := range keys {
		parts = append(parts, `json_extract_string(attributes, '$."`+key+`"')`)
	}
	parts = append(parts, "''")
	return "COALESCE(" + strings.Join(parts, ",") + ")"
}

func tokenValue(value string) *int64 {
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		return &n
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || math.Trunc(f) != f || f >= math.Exp2(63) || f < -math.Exp2(63) {
		return nil
	}
	n := int64(f)
	return &n
}

func QueryGenAICalls(db *sql.DB, opts GenAIOptions) ([]GenAICall, error) {
	if err := validateQueryOptions(opts.QueryOptions); err != nil {
		return nil, err
	}
	columns := []string{"trace_id", "span_id", "COALESCE(parent_span_id,'')", "name", "service_name", "start_time", "COALESCE(duration_ns,0)/1000000.0", "COALESCE(status_code,0)", "COALESCE(e.n,0)"}
	expressions := make(map[string]string, len(genAIAliases))
	for _, alias := range genAIAliases {
		expressions[alias.field] = aliasExpression(alias.keys)
		columns = append(columns, expressions[alias.field])
	}
	errorExpr := `COALESCE(json_extract_string(attributes,'$."error.type"'),CASE WHEN status_code=2 THEN 'error' ELSE '' END)`
	columns = append(columns, errorExpr, `COALESCE(json_extract_string(attributes,'$."gen_ai.response.finish_reasons"'),'')`)
	if opts.Content {
		columns = append(columns, "CAST(attributes AS VARCHAR)")
	}
	query := "SELECT " + strings.Join(columns, ",") + ` FROM traces LEFT JOIN (SELECT lower(trace_id) AS event_trace_id,lower(span_id) AS event_span_id,COUNT(*) AS n FROM logs WHERE starts_with(event_name,'gen_ai.') GROUP BY lower(trace_id),lower(span_id)) e ON lower(traces.trace_id)=e.event_trace_id AND lower(traces.span_id)=e.event_span_id WHERE EXISTS(SELECT 1 FROM unnest(json_keys(attributes)) AS keys(k) WHERE starts_with(k,'gen_ai.'))`
	args := buildWhere(&query, opts.QueryOptions, "start_time")
	if opts.TraceID != "" {
		query += " AND lower(trace_id)=?"
		args = append(args, strings.ToLower(opts.TraceID))
	}
	filters := []struct{ value, expr string }{
		{opts.Provider, expressions["provider"]},
		{opts.Operation, expressions["operation"]},
		{opts.ConversationID, expressions["conversation_id"]},
	}
	for _, filter := range filters {
		if filter.value != "" {
			query += " AND " + filter.expr + "=?"
			args = append(args, filter.value)
		}
	}
	if opts.Model != "" {
		query += " AND (" + expressions["request_model"] + "=? OR " + expressions["response_model"] + "=?)"
		args = append(args, opts.Model, opts.Model)
	}
	if opts.ErrorsOnly {
		query += " AND " + errorExpr + "<>''"
	}
	query += " ORDER BY start_time DESC,trace_id,span_id"
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	var calls []GenAICall
	var contents []map[string]any
	for rows.Next() {
		var call GenAICall
		values := make([]string, len(genAIAliases))
		var reasons, attrs string
		dest := []any{&call.TraceID, &call.SpanID, &call.ParentSpanID, &call.SpanName, &call.ServiceName, &call.StartTime, &call.DurationMS, &call.StatusCode, &call.EventCount}
		for i := range values {
			dest = append(dest, &values[i])
		}
		dest = append(dest, &call.ErrorType, &reasons)
		if opts.Content {
			dest = append(dest, &attrs)
		}
		if err := rows.Scan(dest...); err != nil {
			rows.Close()
			return nil, err
		}
		fields := make(map[string]string, len(genAIAliases))
		for i, alias := range genAIAliases {
			fields[alias.field] = values[i]
		}
		call.Operation = fields["operation"]
		call.Provider = fields["provider"]
		call.RequestModel = fields["request_model"]
		call.ResponseModel = fields["response_model"]
		call.ResponseID = fields["response_id"]
		call.ConversationID = fields["conversation_id"]
		call.AgentName = fields["agent_name"]
		call.ToolName = fields["tool_name"]
		call.InputTokens = tokenValue(fields["input_tokens"])
		call.OutputTokens = tokenValue(fields["output_tokens"])
		call.CacheReadInputTokens = tokenValue(fields["cache_read_input_tokens"])
		call.CacheWriteInputTokens = tokenValue(fields["cache_write_input_tokens"])
		call.ReasoningOutputTokens = tokenValue(fields["reasoning_output_tokens"])
		if reasons != "" {
			_ = json.Unmarshal([]byte(reasons), &call.FinishReasons)
		}
		if opts.Content {
			var content map[string]any
			if err := decodeJSON([]byte(attrs), &content); err != nil {
				rows.Close()
				return nil, err
			}
			contents = append(contents, content)
		}
		calls = append(calls, call)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if opts.Content && len(calls) > 0 {
		events, err := queryContentEvents(db, calls)
		if err != nil {
			return nil, err
		}
		for i := range calls {
			populateContent(&calls[i], contents[i], events[normalizedSpanIdentity(calls[i].TraceID, calls[i].SpanID)])
		}
	}
	return calls, nil
}

func decodeContent(value any) any {
	if text, ok := value.(string); ok {
		var decoded any
		if decodeJSON([]byte(text), &decoded) == nil {
			switch decoded.(type) {
			case []any, map[string]any:
				return decoded
			}
		}
	}
	return value
}

func populateContent(call *GenAICall, attrs map[string]any, logs []LogResult) {
	targets := []struct {
		key  string
		dest *any
	}{
		{"gen_ai.input.messages", &call.InputMessages}, {"gen_ai.output.messages", &call.OutputMessages},
		{"gen_ai.system_instructions", &call.SystemInstructions}, {"gen_ai.tool.definitions", &call.ToolDefinitions},
		{"gen_ai.tool.call.arguments", &call.ToolCallArguments}, {"gen_ai.tool.call.result", &call.ToolCallResult},
	}
	for _, target := range targets {
		if value, ok := attrs[target.key]; ok {
			*target.dest = decodeContent(value)
			call.ContentSource = "span"
			continue
		}
		for _, log := range logs {
			value, ok := log.Attributes[target.key]
			if !ok {
				if body, isMap := log.BodyValue.(map[string]any); isMap {
					value, ok = body[target.key]
				}
			}
			if ok {
				*target.dest = decodeContent(value)
				if call.ContentSource == "" {
					call.ContentSource = "event"
				}
				break
			}
		}
	}
}

type DurationStats struct {
	Avg float64 `json:"avg"`
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	Max float64 `json:"max"`
}
type GenAIUsageRow struct {
	Provider              string        `json:"provider,omitempty"`
	Model                 string        `json:"model,omitempty"`
	Operation             string        `json:"operation,omitempty"`
	Service               string        `json:"service,omitempty"`
	Conversation          string        `json:"conversation,omitempty"`
	Calls                 int           `json:"calls"`
	Errors                int           `json:"errors"`
	InputTokens           int64         `json:"input_tokens"`
	OutputTokens          int64         `json:"output_tokens"`
	CacheReadInputTokens  int64         `json:"cache_read_input_tokens"`
	CacheWriteInputTokens int64         `json:"cache_write_input_tokens"`
	ReasoningOutputTokens int64         `json:"reasoning_output_tokens"`
	CallsMissingUsage     int           `json:"calls_missing_usage"`
	TotalTokens           int64         `json:"total_tokens"`
	DurationMS            DurationStats `json:"duration_ms"`
}

func QueryGenAIUsage(db *sql.DB, opts GenAIOptions) ([]GenAIUsageRow, error) {
	groups := opts.GroupBy
	if len(groups) == 0 {
		groups = []string{"provider", "model", "operation"}
	}
	for _, g := range groups {
		switch g {
		case "provider", "model", "operation", "service", "conversation":
		default:
			return nil, fmt.Errorf("invalid group-by %q", g)
		}
	}
	opts.Limit = 0
	opts.Content = false
	calls, err := QueryGenAICalls(db, opts)
	if err != nil {
		return nil, err
	}
	type aggregate struct {
		row       GenAIUsageRow
		durations []float64
	}
	aggregates := map[string]*aggregate{}
	for _, call := range calls {
		model := call.ResponseModel
		if model == "" {
			model = call.RequestModel
		}
		values := map[string]string{"provider": call.Provider, "model": model, "operation": call.Operation, "service": call.ServiceName, "conversation": call.ConversationID}
		keyValues := make([]string, len(groups))
		for i, g := range groups {
			keyValues[i] = values[g]
		}
		key := jsonText(keyValues)
		agg := aggregates[key]
		if agg == nil {
			agg = &aggregate{}
			for _, g := range groups {
				switch g {
				case "provider":
					agg.row.Provider = values[g]
				case "model":
					agg.row.Model = values[g]
				case "operation":
					agg.row.Operation = values[g]
				case "service":
					agg.row.Service = values[g]
				case "conversation":
					agg.row.Conversation = values[g]
				}
			}
			aggregates[key] = agg
		}
		agg.row.Calls++
		if call.ErrorType != "" {
			agg.row.Errors++
		}
		if call.InputTokens == nil && call.OutputTokens == nil {
			agg.row.CallsMissingUsage++
		}
		for _, pair := range []struct {
			src  *int64
			dest *int64
		}{{call.InputTokens, &agg.row.InputTokens}, {call.OutputTokens, &agg.row.OutputTokens}, {call.CacheReadInputTokens, &agg.row.CacheReadInputTokens}, {call.CacheWriteInputTokens, &agg.row.CacheWriteInputTokens}, {call.ReasoningOutputTokens, &agg.row.ReasoningOutputTokens}} {
			if pair.src != nil {
				*pair.dest += *pair.src
			}
		}
		agg.durations = append(agg.durations, call.DurationMS)
	}
	var results []GenAIUsageRow
	for _, agg := range aggregates {
		sort.Float64s(agg.durations)
		var total float64
		for _, d := range agg.durations {
			total += d
		}
		n := len(agg.durations)
		agg.row.DurationMS = DurationStats{Avg: total / float64(n), P50: agg.durations[int(math.Ceil(float64(n)*.5))-1], P95: agg.durations[int(math.Ceil(float64(n)*.95))-1], Max: agg.durations[n-1]}
		agg.row.TotalTokens = agg.row.InputTokens + agg.row.OutputTokens
		results = append(results, agg.row)
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].TotalTokens != results[j].TotalTokens {
			return results[i].TotalTokens > results[j].TotalTokens
		}
		a, b := results[i], results[j]
		av := map[string]string{"provider": a.Provider, "model": a.Model, "operation": a.Operation, "service": a.Service, "conversation": a.Conversation}
		bv := map[string]string{"provider": b.Provider, "model": b.Model, "operation": b.Operation, "service": b.Service, "conversation": b.Conversation}
		for _, g := range groups {
			if av[g] != bv[g] {
				return av[g] < bv[g]
			}
		}
		return false
	})
	return results, nil
}

type spanIdentity struct{ traceID, spanID string }

func normalizedSpanIdentity(traceID, spanID string) spanIdentity {
	return spanIdentity{strings.ToLower(traceID), strings.ToLower(spanID)}
}

func queryContentEvents(db *sql.DB, calls []GenAICall) (map[spanIdentity][]LogResult, error) {
	events := make(map[spanIdentity][]LogResult, len(calls))
	traces := map[string]bool{}
	var args []any
	var placeholders []string
	for _, call := range calls {
		identity := normalizedSpanIdentity(call.TraceID, call.SpanID)
		events[identity] = nil
		if !traces[identity.traceID] {
			traces[identity.traceID] = true
			placeholders = append(placeholders, "?")
			args = append(args, identity.traceID)
		}
	}
	query := `SELECT trace_id,span_id,CAST(attributes AS VARCHAR),CAST(body_json AS VARCHAR) FROM logs WHERE event_name='gen_ai.client.inference.operation.details' AND span_id IS NOT NULL AND lower(trace_id) IN (` + strings.Join(placeholders, ",") + ") ORDER BY timestamp"
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var log LogResult
		var attrs, body sql.NullString
		if err := rows.Scan(&log.TraceID, &log.SpanID, &attrs, &body); err != nil {
			return nil, err
		}
		identity := normalizedSpanIdentity(log.TraceID, log.SpanID)
		if _, selected := events[identity]; !selected {
			continue
		}
		if attrs.Valid {
			if err := decodeJSON([]byte(attrs.String), &log.Attributes); err != nil {
				return nil, err
			}
		}
		if body.Valid {
			if err := decodeJSON([]byte(body.String), &log.BodyValue); err != nil {
				return nil, err
			}
		}
		events[identity] = append(events[identity], log)
	}
	return events, rows.Err()
}
