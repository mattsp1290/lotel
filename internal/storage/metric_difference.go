package storage

import (
	"bytes"
	"database/sql"
	"fmt"
	"math/big"
	"sort"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

type MetricDifference struct {
	FromUnixNano string   `json:"from_unix_nano"`
	ToUnixNano   string   `json:"to_unix_nano"`
	Difference   string   `json:"difference"`
	ElapsedNano  string   `json:"elapsed_nanoseconds"`
	Assumptions  []string `json:"assumptions"`
}

const (
	MetricSelectorByteLimit  = 4 << 20
	metricEnvelopeLimit      = 100000
	metricEnvelopeByteLimit  = 4 << 20
	metricTotalByteLimit     = 64 << 20
	metricPointLimit         = 100000
	metricJSONTokenLimit     = 1000000
	metricAttributeLimit     = 10000
	metricJSONRecursionLimit = 100
)

type metricObservation struct {
	timestamp uint64
	start     uint64
	value     int64
	flags     uint32
	integer   bool
}

type metricIdentityRelation uint8

const (
	metricIdentityMatch metricIdentityRelation = iota
	metricIdentityDisjoint
	metricIdentityUncertain
)

func metricResourceRelation(resource *resourcepb.Resource, id metricSelection) metricIdentityRelation {
	key, err := canonicalResource(resource, false)
	if err == nil {
		if bytes.Equal(key, id.resource) {
			return metricIdentityMatch
		}
		return metricIdentityDisjoint
	}
	if resource != nil && knownAttributesDisjoint(resource.Attributes, id.resourceAttrs) {
		return metricIdentityDisjoint
	}
	return metricIdentityUncertain
}

func metricScopeRelation(scope *commonpb.InstrumentationScope, id metricSelection) metricIdentityRelation {
	if scope == nil {
		if id.scopeName != "" || id.scopeVersion != "" {
			return metricIdentityDisjoint
		}
	} else if scope.Name != id.scopeName || scope.Version != id.scopeVersion {
		return metricIdentityDisjoint
	}
	key, err := canonicalScope(scope, false)
	if err == nil {
		if bytes.Equal(key, id.scope) {
			return metricIdentityMatch
		}
		return metricIdentityDisjoint
	}
	if scope != nil && knownAttributesDisjoint(scope.Attributes, id.scopeAttrs) {
		return metricIdentityDisjoint
	}
	return metricIdentityUncertain
}

func metricPointRelation(attrs []*commonpb.KeyValue, id metricSelection) metricIdentityRelation {
	key, err := canonicalPoint(attrs, false)
	if err == nil {
		if bytes.Equal(key, id.attributes) {
			return metricIdentityMatch
		}
		return metricIdentityDisjoint
	}
	if knownAttributesDisjoint(attrs, id.pointAttrs) {
		return metricIdentityDisjoint
	}
	return metricIdentityUncertain
}

type metricPointAttributes interface{ GetAttributes() []*commonpb.KeyValue }

func visitMetricPoints(metric *metricpb.Metric, visit func(metricPointAttributes, *metricpb.NumberDataPoint, bool) error) error {
	switch data := metric.Data.(type) {
	case *metricpb.Metric_Sum:
		if data.Sum == nil {
			return fmt.Errorf("indeterminate: missing sum")
		}
		valid := data.Sum.IsMonotonic && data.Sum.AggregationTemporality == metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE
		for _, point := range data.Sum.DataPoints {
			if point == nil {
				return fmt.Errorf("indeterminate: missing point")
			}
			if err := visit(point, point, valid); err != nil {
				return err
			}
		}
	case *metricpb.Metric_Gauge:
		if data.Gauge == nil {
			return fmt.Errorf("indeterminate: missing gauge")
		}
		for _, point := range data.Gauge.DataPoints {
			if point == nil {
				return fmt.Errorf("indeterminate: missing point")
			}
			if err := visit(point, nil, false); err != nil {
				return err
			}
		}
	case *metricpb.Metric_Histogram:
		if data.Histogram == nil {
			return fmt.Errorf("indeterminate: missing histogram")
		}
		for _, point := range data.Histogram.DataPoints {
			if point == nil {
				return fmt.Errorf("indeterminate: missing point")
			}
			if err := visit(point, nil, false); err != nil {
				return err
			}
		}
	case *metricpb.Metric_ExponentialHistogram:
		if data.ExponentialHistogram == nil {
			return fmt.Errorf("indeterminate: missing exponential histogram")
		}
		for _, point := range data.ExponentialHistogram.DataPoints {
			if point == nil {
				return fmt.Errorf("indeterminate: missing point")
			}
			if err := visit(point, nil, false); err != nil {
				return err
			}
		}
	case *metricpb.Metric_Summary:
		if data.Summary == nil {
			return fmt.Errorf("indeterminate: missing summary")
		}
		for _, point := range data.Summary.DataPoints {
			if point == nil {
				return fmt.Errorf("indeterminate: missing point")
			}
			if err := visit(point, nil, false); err != nil {
				return err
			}
		}
	case nil:
		return nil
	default:
		return fmt.Errorf("indeterminate: unsupported metric type")
	}
	return nil
}

func observedMetricPoints(db *sql.DB, selector []byte, from, to uint64) ([]metricObservation, error) {
	id, err := parseMetricSelection(selector)
	if err != nil {
		return nil, fmt.Errorf("invalid selector: %w", err)
	}
	if from == 0 || to == 0 || from >= to {
		return nil, fmt.Errorf("point timestamps must be nonzero and increasing")
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	size, err := metricEnvelopePreflight(tx)
	if err != nil {
		return nil, err
	}
	var points []metricObservation
	budget := metricJSONBudget{}
	err = visitMetricEnvelopes(tx, size, func(raw []byte) error {
		if err := budget.scan(raw); err != nil {
			return err
		}
		data, err := decodeMetricEnvelope(raw)
		if err != nil {
			return fmt.Errorf("indeterminate: unsupported or malformed retained metrics envelope: %w", err)
		}
		if err := validateMetricEnvelope(data); err != nil {
			return err
		}
		for _, resource := range data.ResourceMetrics {
			if resource == nil {
				return fmt.Errorf("indeterminate: missing resource envelope")
			}
			if resource.SchemaUrl != id.resourceSchema {
				continue
			}
			resourceRelation := metricResourceRelation(resource.Resource, id)
			if resourceRelation == metricIdentityDisjoint {
				continue
			}
			for _, scope := range resource.ScopeMetrics {
				if scope == nil {
					return fmt.Errorf("indeterminate: missing scope envelope")
				}
				if scope.SchemaUrl != id.scopeSchema {
					continue
				}
				scopeRelation := metricScopeRelation(scope.Scope, id)
				if scopeRelation == metricIdentityDisjoint {
					continue
				}
				for _, metric := range scope.Metrics {
					if metric == nil {
						return fmt.Errorf("indeterminate: missing metric")
					}
					if metric.Name != id.name || metric.Unit != id.unit {
						continue
					}
					err := visitMetricPoints(metric, func(attrs metricPointAttributes, point *metricpb.NumberDataPoint, validType bool) error {
						pointRelation := metricPointRelation(attrs.GetAttributes(), id)
						if pointRelation == metricIdentityDisjoint {
							return nil
						}
						if resourceRelation != metricIdentityMatch || scopeRelation != metricIdentityMatch || pointRelation != metricIdentityMatch {
							return fmt.Errorf("indeterminate: possibly matching incomplete identity")
						}
						if !validType || point == nil {
							return fmt.Errorf("indeterminate: conflicting metric type or temporality")
						}
						if point.TimeUnixNano == 0 {
							return fmt.Errorf("indeterminate: matching point has no timestamp")
						}
						if point.StartTimeUnixNano > point.TimeUnixNano {
							return fmt.Errorf("indeterminate: point has invalid start time")
						}
						value, ok := point.Value.(*metricpb.NumberDataPoint_AsInt)
						observation := metricObservation{timestamp: point.TimeUnixNano, start: point.StartTimeUnixNano, flags: point.Flags, integer: ok}
						if ok {
							observation.value = value.AsInt
						}
						points = append(points, observation)
						return nil
					})
					if err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
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
	sort.Slice(points, func(i, j int) bool { return points[i].timestamp < points[j].timestamp })
	var first, last *metricObservation
	for i := range points {
		if points[i].timestamp == from {
			if first != nil {
				return result, fmt.Errorf("indeterminate: duplicate first endpoint")
			}
			first = &points[i]
		}
		if points[i].timestamp == to {
			if last != nil {
				return result, fmt.Errorf("indeterminate: duplicate second endpoint")
			}
			last = &points[i]
		}
	}
	if first == nil || last == nil {
		return result, fmt.Errorf("indeterminate: selected endpoint points are missing")
	}
	if !first.integer || first.flags != 0 || first.start == 0 || first.start > from || !last.integer || last.flags != 0 || last.start != first.start || last.value < first.value {
		return result, fmt.Errorf("indeterminate: invalid endpoints or reset")
	}
	start := first.start
	var previous *metricObservation
	for i := range points {
		point := &points[i]
		if point.start > point.timestamp {
			return result, fmt.Errorf("indeterminate: point has invalid start time")
		}
		if point.start == 0 {
			if point.timestamp > from {
				return result, fmt.Errorf("indeterminate: point has unknown start")
			}
			continue
		}
		if point.start != start {
			if point.timestamp > from && point.start < to {
				return result, fmt.Errorf("indeterminate: overlapping cumulative epoch")
			}
			continue
		}
		if !point.integer || point.flags != 0 {
			return result, fmt.Errorf("indeterminate: conflicting cumulative point")
		}
		if previous != nil && (point.timestamp == previous.timestamp || point.value < previous.value) {
			return result, fmt.Errorf("indeterminate: duplicate or decreasing cumulative point")
		}
		previous = point
	}
	difference := new(big.Int).Sub(big.NewInt(last.value), big.NewInt(first.value))
	result = MetricDifference{FromUnixNano: fmt.Sprint(from), ToUnixNano: fmt.Sprint(to), Difference: difference.String(), ElapsedNano: fmt.Sprint(to - from), Assumptions: []string{"Observed retained points only; ingestion or retention may be incomplete.", "The selected stream is assumed to have a single writer."}}
	return result, nil
}
