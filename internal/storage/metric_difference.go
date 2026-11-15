package storage

import (
	"database/sql"
	"fmt"
	"math"
	"math/big"
	"sort"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type MetricDifference struct {
	FromUnixNano string   `json:"from_unix_nano"`
	ToUnixNano   string   `json:"to_unix_nano"`
	Difference   string   `json:"difference"`
	ElapsedNano  string   `json:"elapsed_nanoseconds"`
	Assumptions  []string `json:"assumptions"`
}

type metricSelection struct {
	resource       []byte
	resourceSchema string
	scope          []byte
	scopeSchema    string
	name           string
	unit           string
	attributes     []byte
}

func canonicalAttrs(attrs []*commonpb.KeyValue, rejectNonfinite bool) error {
	seen := make(map[string]bool, len(attrs))
	for _, kv := range attrs {
		if kv == nil || kv.Key == "" || kv.KeyStrindex != 0 || kv.Value == nil || seen[kv.Key] {
			return fmt.Errorf("invalid or duplicate attribute key")
		}
		seen[kv.Key] = true
		if err := canonicalValue(kv.Value, rejectNonfinite); err != nil {
			return err
		}
	}
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].Key < attrs[j].Key })
	return nil
}

func canonicalValue(v *commonpb.AnyValue, rejectNonfinite bool) error {
	if v == nil {
		return fmt.Errorf("missing attribute value")
	}
	switch x := v.Value.(type) {
	case *commonpb.AnyValue_StringValue, *commonpb.AnyValue_BoolValue, *commonpb.AnyValue_IntValue, *commonpb.AnyValue_BytesValue:
		return nil
	case *commonpb.AnyValue_DoubleValue:
		if rejectNonfinite && (math.IsNaN(x.DoubleValue) || math.IsInf(x.DoubleValue, 0)) {
			return fmt.Errorf("non-finite attribute value")
		}
		if x.DoubleValue == 0 {
			x.DoubleValue = 0
		}
		return nil
	case *commonpb.AnyValue_ArrayValue:
		if x.ArrayValue == nil {
			return fmt.Errorf("invalid array attribute")
		}
		for _, item := range x.ArrayValue.Values {
			if err := canonicalValue(item, rejectNonfinite); err != nil {
				return err
			}
		}
		return nil
	case *commonpb.AnyValue_KvlistValue:
		if x.KvlistValue == nil {
			return fmt.Errorf("invalid map attribute")
		}
		return canonicalAttrs(x.KvlistValue.Values, rejectNonfinite)
	default:
		return fmt.Errorf("unsupported attribute value")
	}
}

func encodedIdentity(m proto.Message) ([]byte, error) {
	return proto.MarshalOptions{Deterministic: true}.Marshal(m)
}

func canonicalResource(r *resourcepb.Resource, rejectNonfinite bool) ([]byte, error) {
	if r == nil {
		r = &resourcepb.Resource{}
	} else {
		r = proto.Clone(r).(*resourcepb.Resource)
	}
	if r.DroppedAttributesCount != 0 || len(r.EntityRefs) != 0 {
		return nil, fmt.Errorf("resource has dropped attributes or unsupported entity references")
	}
	if err := canonicalAttrs(r.Attributes, rejectNonfinite); err != nil {
		return nil, err
	}
	return encodedIdentity(r)
}
func canonicalScope(s *commonpb.InstrumentationScope, rejectNonfinite bool) ([]byte, error) {
	if s == nil {
		s = &commonpb.InstrumentationScope{}
	} else {
		s = proto.Clone(s).(*commonpb.InstrumentationScope)
	}
	if s.DroppedAttributesCount != 0 {
		return nil, fmt.Errorf("scope has dropped attributes")
	}
	if err := canonicalAttrs(s.Attributes, rejectNonfinite); err != nil {
		return nil, err
	}
	return encodedIdentity(s)
}
func canonicalPoint(attrs []*commonpb.KeyValue, rejectNonfinite bool) ([]byte, error) {
	p := &metricpb.NumberDataPoint{Attributes: attrs}
	p = proto.Clone(p).(*metricpb.NumberDataPoint)
	if err := canonicalAttrs(p.Attributes, rejectNonfinite); err != nil {
		return nil, err
	}
	return encodedIdentity(p)
}
func decodeMetricEnvelope(data []byte) (*metricpb.MetricsData, error) {
	m := &metricpb.MetricsData{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(data, m); err != nil {
		return nil, err
	}
	return m, nil
}
func parseMetricSelection(data []byte) (metricSelection, error) {
	var id metricSelection
	m, err := decodeMetricEnvelope(data)
	if err != nil {
		return id, err
	}
	if len(m.ResourceMetrics) != 1 || m.ResourceMetrics[0] == nil {
		return id, fmt.Errorf("selector must contain one resource")
	}
	r := m.ResourceMetrics[0]
	if len(r.ScopeMetrics) != 1 || r.ScopeMetrics[0] == nil {
		return id, fmt.Errorf("selector must contain one scope")
	}
	s := r.ScopeMetrics[0]
	if len(s.Metrics) != 1 || s.Metrics[0] == nil {
		return id, fmt.Errorf("selector must contain one metric")
	}
	metric := s.Metrics[0]
	sum := metric.GetSum()
	if metric.Name == "" || sum == nil || !sum.IsMonotonic || sum.AggregationTemporality != metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE || len(sum.DataPoints) != 1 || sum.DataPoints[0] == nil {
		return id, fmt.Errorf("selector must contain one cumulative monotonic sum point")
	}
	if id.resource, err = canonicalResource(r.Resource, true); err != nil {
		return id, err
	}
	if id.scope, err = canonicalScope(s.Scope, true); err != nil {
		return id, err
	}
	if id.attributes, err = canonicalPoint(sum.DataPoints[0].Attributes, true); err != nil {
		return id, err
	}
	id.resourceSchema, id.scopeSchema, id.name, id.unit = r.SchemaUrl, s.SchemaUrl, metric.Name, metric.Unit
	return id, nil
}

func ValidateMetricSelector(data []byte) error {
	_, err := parseMetricSelection(data)
	return err
}

func observedMetricPoints(db *sql.DB, selector []byte, from, to uint64) ([]*metricpb.NumberDataPoint, error) {
	id, err := parseMetricSelection(selector)
	if err != nil {
		return nil, fmt.Errorf("invalid selector: %w", err)
	}
	if from == 0 || to == 0 || from >= to {
		return nil, fmt.Errorf("point timestamps must be nonzero and increasing")
	}
	rows, err := db.Query(`SELECT CAST(payload AS VARCHAR) FROM otlp_envelopes WHERE signal = 'metrics'`)
	if err != nil {
		return nil, fmt.Errorf("querying metric envelopes: %w", err)
	}
	defer rows.Close()
	var points []*metricpb.NumberDataPoint
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		data, err := decodeMetricEnvelope([]byte(raw))
		if err != nil {
			return nil, fmt.Errorf("indeterminate: invalid retained metrics envelope: %w", err)
		}
		for _, resource := range data.ResourceMetrics {
			if resource == nil {
				return nil, fmt.Errorf("indeterminate: missing resource envelope")
			}
			if resource.SchemaUrl != id.resourceSchema {
				continue
			}
			resourceKey, err := canonicalResource(resource.Resource, false)
			if err != nil {
				return nil, fmt.Errorf("indeterminate: %w", err)
			}
			if string(resourceKey) != string(id.resource) {
				continue
			}
			for _, scope := range resource.ScopeMetrics {
				if scope == nil {
					return nil, fmt.Errorf("indeterminate: missing scope envelope")
				}
				if scope.SchemaUrl != id.scopeSchema {
					continue
				}
				scopeKey, err := canonicalScope(scope.Scope, false)
				if err != nil {
					return nil, fmt.Errorf("indeterminate: %w", err)
				}
				if string(scopeKey) != string(id.scope) {
					continue
				}
				for _, metric := range scope.Metrics {
					if metric == nil {
						return nil, fmt.Errorf("indeterminate: missing metric")
					}
					if metric.Name != id.name || metric.Unit != id.unit {
						continue
					}
					sum := metric.GetSum()
					if sum == nil || !sum.IsMonotonic || sum.AggregationTemporality != metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE {
						return nil, fmt.Errorf("indeterminate: conflicting metric type or temporality")
					}
					for _, point := range sum.DataPoints {
						if point == nil {
							return nil, fmt.Errorf("indeterminate: missing data point")
						}
						key, err := canonicalPoint(point.Attributes, false)
						if err != nil {
							return nil, fmt.Errorf("indeterminate: %w", err)
						}
						if string(key) != string(id.attributes) {
							continue
						}
						if point.TimeUnixNano == 0 {
							return nil, fmt.Errorf("indeterminate: matching point has no timestamp")
						}
						if point.TimeUnixNano < from || point.TimeUnixNano > to {
							continue
						}
						points = append(points, point)
					}
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return points, nil
}

func QueryMetricDifference(db *sql.DB, selector []byte, from, to uint64) (MetricDifference, error) {
	result := MetricDifference{}
	points, err := observedMetricPoints(db, selector, from, to)
	if err != nil {
		return result, err
	}
	sort.Slice(points, func(i, j int) bool { return points[i].TimeUnixNano < points[j].TimeUnixNano })
	if len(points) < 2 || points[0].TimeUnixNano != from || points[len(points)-1].TimeUnixNano != to {
		return result, fmt.Errorf("indeterminate: selected endpoint points are missing")
	}
	var start uint64
	var previous int64
	for i, point := range points {
		value, ok := point.Value.(*metricpb.NumberDataPoint_AsInt)
		if !ok || point.Flags != 0 || point.StartTimeUnixNano == 0 || point.StartTimeUnixNano > point.TimeUnixNano {
			return result, fmt.Errorf("indeterminate: point has invalid value, flags, or start")
		}
		if i == 0 {
			start = point.StartTimeUnixNano
		} else if point.TimeUnixNano == points[i-1].TimeUnixNano || point.StartTimeUnixNano != start || value.AsInt < previous {
			return result, fmt.Errorf("indeterminate: duplicate point, reset, or decreasing value")
		}
		previous = value.AsInt
	}
	first := points[0].Value.(*metricpb.NumberDataPoint_AsInt).AsInt
	last := points[len(points)-1].Value.(*metricpb.NumberDataPoint_AsInt).AsInt
	difference := new(big.Int).Sub(big.NewInt(last), big.NewInt(first))
	result = MetricDifference{FromUnixNano: fmt.Sprint(from), ToUnixNano: fmt.Sprint(to), Difference: difference.String(), ElapsedNano: fmt.Sprint(to - from), Assumptions: []string{"Observed retained points only; ingestion or retention may be incomplete.", "The selected stream is assumed to have a single writer."}}
	return result, nil
}
