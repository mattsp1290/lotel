package storage

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"sort"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type metricSelection struct {
	resourceAttrs  map[string][]byte
	scopeAttrs     map[string][]byte
	pointAttrs     map[string][]byte
	scopeName      string
	scopeVersion   string
	resource       []byte
	resourceSchema string
	scope          []byte
	scopeSchema    string
	name           string
	unit           string
	attributes     []byte
}

func canonicalAttrs(attrs []*commonpb.KeyValue, rejectNonfinite bool) error {
	if len(attrs) > metricAttributeLimit {
		return fmt.Errorf("attribute count limit exceeded")
	}
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
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false, RecursionLimit: metricJSONRecursionLimit}).Unmarshal(data, m); err != nil {
		return nil, err
	}
	return m, nil
}
func parseMetricSelection(ctx context.Context, data []byte, temporality metricpb.AggregationTemporality, kind metricKind) (metricSelection, error) {
	var id metricSelection
	if len(data) > MetricSelectorByteLimit {
		return id, fmt.Errorf("metric selector exceeds size limit")
	}
	budget := metricJSONBudget{maxPoints: 1, ctx: ctx}
	if err := budget.scan(data); err != nil {
		return id, err
	}
	m, err := decodeMetricEnvelope(data)
	if err != nil {
		return id, err
	}
	if err := ctx.Err(); err != nil {
		return id, err
	}
	if err := validateMetricEnvelope(ctx, m); err != nil {
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
	var pointAttrs []*commonpb.KeyValue
	if kind == metricHistogram {
		histogram := metric.GetHistogram()
		if metric.Name == "" || histogram == nil || histogram.AggregationTemporality != temporality || len(histogram.DataPoints) != 1 || histogram.DataPoints[0] == nil {
			return id, fmt.Errorf("selector must contain one explicit histogram point with the requested temporality")
		}
		pointAttrs = histogram.DataPoints[0].Attributes
	} else {
		sum := metric.GetSum()
		if metric.Name == "" || sum == nil || !sum.IsMonotonic || sum.AggregationTemporality != temporality || len(sum.DataPoints) != 1 || sum.DataPoints[0] == nil {
			if temporality == metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA {
				return id, fmt.Errorf("selector must contain one delta monotonic sum point")
			}
			return id, fmt.Errorf("selector must contain one cumulative monotonic sum point")
		}
		pointAttrs = sum.DataPoints[0].Attributes
	}
	if id.resource, err = canonicalResource(r.Resource, true); err != nil {
		return id, err
	}
	if id.scope, err = canonicalScope(s.Scope, true); err != nil {
		return id, err
	}
	if id.attributes, err = canonicalPoint(pointAttrs, true); err != nil {
		return id, err
	}
	id.resourceSchema, id.scopeSchema, id.name, id.unit = r.SchemaUrl, s.SchemaUrl, metric.Name, metric.Unit
	var resourceAttrs []*commonpb.KeyValue
	if r.Resource != nil {
		resourceAttrs = r.Resource.Attributes
	}
	id.resourceAttrs, err = canonicalAttributeMap(resourceAttrs)
	if err != nil {
		return id, err
	}
	if s.Scope != nil {
		id.scopeName, id.scopeVersion = s.Scope.Name, s.Scope.Version
		id.scopeAttrs, err = canonicalAttributeMap(s.Scope.Attributes)
		if err != nil {
			return id, err
		}
	}
	id.pointAttrs, err = canonicalAttributeMap(pointAttrs)
	if err != nil {
		return id, err
	}
	return id, nil
}

type metricKind uint8

const (
	metricSum metricKind = iota
	metricHistogram
)

type MetricSelector struct {
	identity    metricSelection
	kind        metricKind
	temporality metricpb.AggregationTemporality
}

func PrepareMetricSelector(ctx context.Context, data []byte) (MetricSelector, error) {
	return prepareMetricSelector(ctx, data, metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE, metricSum)
}

func PrepareDeltaMetricSelector(ctx context.Context, data []byte) (MetricSelector, error) {
	return prepareMetricSelector(ctx, data, metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA, metricSum)
}

func PrepareDeltaHistogramSelector(ctx context.Context, data []byte) (MetricSelector, error) {
	return prepareMetricSelector(ctx, data, metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA, metricHistogram)
}

func PrepareCumulativeHistogramSelector(ctx context.Context, data []byte) (MetricSelector, error) {
	return prepareMetricSelector(ctx, data, metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE, metricHistogram)
}

func prepareMetricSelector(ctx context.Context, data []byte, temporality metricpb.AggregationTemporality, kind metricKind) (MetricSelector, error) {
	id, err := parseMetricSelection(ctx, data, temporality, kind)
	return MetricSelector{identity: id, temporality: temporality, kind: kind}, err
}

func canonicalAttributeMap(attrs []*commonpb.KeyValue) (map[string][]byte, error) {
	result := make(map[string][]byte, len(attrs))
	for _, kv := range attrs {
		clone := proto.Clone(kv).(*commonpb.KeyValue)
		if err := canonicalAttrs([]*commonpb.KeyValue{clone}, true); err != nil {
			return nil, err
		}
		value, err := encodedIdentity(clone)
		if err != nil {
			return nil, err
		}
		result[kv.Key] = value
	}
	return result, nil
}

func knownAttributesDisjoint(candidate []*commonpb.KeyValue, selectedByKey map[string][]byte) bool {
	counts := make(map[string]int, len(candidate))
	for _, kv := range candidate {
		if kv != nil {
			counts[kv.Key]++
		}
	}
	for _, kv := range candidate {
		if kv == nil || counts[kv.Key] != 1 {
			continue
		}
		clone := proto.Clone(kv).(*commonpb.KeyValue)
		if canonicalAttrs([]*commonpb.KeyValue{clone}, false) != nil {
			continue
		}
		value, err := encodedIdentity(clone)
		if err != nil {
			continue
		}
		selectedValue, ok := selectedByKey[kv.Key]
		if !ok || !bytes.Equal(value, selectedValue) {
			return true
		}
	}
	return false
}
