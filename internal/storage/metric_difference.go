package storage

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"sort"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

type MetricDifference struct {
	FromUnixNano  string   `json:"from_unix_nano"`
	ToUnixNano    string   `json:"to_unix_nano"`
	Difference    string   `json:"difference"`
	RatePerSecond string   `json:"rate_per_second"`
	ElapsedNano   string   `json:"elapsed_nanoseconds"`
	Assumptions   []string `json:"assumptions"`
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
	histogram *metricpb.HistogramDataPoint
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

func visitMetricPoints(metric *metricpb.Metric, selector MetricSelector, visit func(metricPointAttributes, bool) error) error {
	switch data := metric.Data.(type) {
	case *metricpb.Metric_Sum:
		if data.Sum == nil {
			return fmt.Errorf("indeterminate: missing sum")
		}
		valid := selector.kind == metricSum && data.Sum.IsMonotonic && data.Sum.AggregationTemporality == selector.temporality
		for _, point := range data.Sum.DataPoints {
			if point == nil {
				return fmt.Errorf("indeterminate: missing point")
			}
			if err := visit(point, valid); err != nil {
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
			if err := visit(point, false); err != nil {
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
			if err := visit(point, selector.kind == metricHistogram && data.Histogram.AggregationTemporality == selector.temporality); err != nil {
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
			if err := visit(point, false); err != nil {
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
			if err := visit(point, false); err != nil {
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

func observedMetricPoints(ctx context.Context, db *sql.DB, selector MetricSelector, from, to uint64) ([]metricObservation, error) {
	if from == 0 || to == 0 || from >= to {
		return nil, fmt.Errorf("point timestamps must be nonzero and increasing")
	}
	id := selector.identity
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	size, err := metricEnvelopePreflight(ctx, tx)
	if err != nil {
		return nil, err
	}
	var points []metricObservation
	budget := metricJSONBudget{ctx: ctx}
	err = visitMetricEnvelopes(ctx, tx, size, func(raw []byte) error {
		if err := budget.scan(raw); err != nil {
			return err
		}
		data, err := decodeMetricEnvelope(raw)
		if err != nil {
			return fmt.Errorf("indeterminate: unsupported or malformed retained metrics envelope: %w", err)
		}
		if err := validateMetricEnvelope(ctx, data); err != nil {
			return err
		}
		for _, resource := range data.ResourceMetrics {
			if err := ctx.Err(); err != nil {
				return err
			}
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
				if err := ctx.Err(); err != nil {
					return err
				}
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
					if err := ctx.Err(); err != nil {
						return err
					}
					if metric == nil {
						return fmt.Errorf("indeterminate: missing metric")
					}
					if metric.Name != id.name || metric.Unit != id.unit {
						continue
					}
					err := visitMetricPoints(metric, selector, func(attrs metricPointAttributes, validType bool) error {
						if err := ctx.Err(); err != nil {
							return err
						}
						pointRelation := metricPointRelation(attrs.GetAttributes(), id)
						if pointRelation == metricIdentityDisjoint {
							return nil
						}
						if resourceRelation != metricIdentityMatch || scopeRelation != metricIdentityMatch || pointRelation != metricIdentityMatch {
							return fmt.Errorf("indeterminate: possibly matching incomplete identity")
						}
						if !validType {
							return fmt.Errorf("indeterminate: conflicting metric type or temporality")
						}
						var observation metricObservation
						switch point := attrs.(type) {
						case *metricpb.NumberDataPoint:
							value, ok := point.Value.(*metricpb.NumberDataPoint_AsInt)
							observation = metricObservation{timestamp: point.TimeUnixNano, start: point.StartTimeUnixNano, flags: point.Flags, integer: ok}
							if ok {
								observation.value = value.AsInt
							}
						case *metricpb.HistogramDataPoint:
							observation = metricObservation{timestamp: point.TimeUnixNano, start: point.StartTimeUnixNano, flags: point.Flags, histogram: point}
						default:
							return fmt.Errorf("indeterminate: conflicting metric type")
						}
						if observation.timestamp == 0 {
							return fmt.Errorf("indeterminate: matching point has no timestamp")
						}
						if observation.start > observation.timestamp {
							return fmt.Errorf("indeterminate: point has invalid start time")
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

func QueryMetricDifference(ctx context.Context, db *sql.DB, selector MetricSelector, from, to uint64) (MetricDifference, error) {
	result := MetricDifference{}
	if selector.kind != metricSum || selector.temporality != metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE {
		return result, fmt.Errorf("invalid cumulative metric selector")
	}
	points, err := observedMetricPoints(ctx, db, selector, from, to)
	if err != nil {
		return result, err
	}
	epoch, err := observedCumulativeEpoch(ctx, points, from, to)
	if err != nil {
		return result, err
	}
	for i, point := range epoch.points {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !point.integer || (i > 0 && point.value < epoch.points[i-1].value) {
			return result, fmt.Errorf("indeterminate: invalid or decreasing cumulative integer")
		}
	}
	difference := new(big.Int).Sub(big.NewInt(epoch.points[epoch.last].value), big.NewInt(epoch.points[epoch.first].value))
	return MetricDifference{
		FromUnixNano: fmt.Sprint(from), ToUnixNano: fmt.Sprint(to), Difference: difference.String(),
		ElapsedNano: fmt.Sprint(to - from), RatePerSecond: observedRate(difference, to-from), Assumptions: metricAssumptions(),
	}, nil
}

func observedRate(increase *big.Int, elapsedNs uint64) string {
	perSecond := new(big.Int).Mul(increase, big.NewInt(1_000_000_000))
	return new(big.Rat).SetFrac(perSecond, new(big.Int).SetUint64(elapsedNs)).RatString()
}

func metricAssumptions() []string {
	return []string{"Observed retained points only; ingestion or retention may be incomplete.", "The selected stream is assumed to have a single writer."}
}

type cumulativeEpoch struct {
	points      []metricObservation
	first, last int
}

func observedCumulativeEpoch(ctx context.Context, points []metricObservation, from, to uint64) (cumulativeEpoch, error) {
	sort.Slice(points, func(i, j int) bool { return points[i].timestamp < points[j].timestamp })
	var start uint64
	for _, point := range points {
		if point.timestamp == from {
			start = point.start
			break
		}
	}
	if start == 0 {
		return cumulativeEpoch{}, fmt.Errorf("indeterminate: first endpoint or its start time is missing")
	}
	epoch := cumulativeEpoch{first: -1, last: -1}
	for i, point := range points {
		if err := ctx.Err(); err != nil {
			return cumulativeEpoch{}, err
		}
		if i > 0 && point.timestamp == points[i-1].timestamp && (point.timestamp == from || point.timestamp == to) {
			return cumulativeEpoch{}, fmt.Errorf("indeterminate: duplicate endpoint")
		}
		if point.start != start {
			if point.start == 0 && point.timestamp > from {
				return cumulativeEpoch{}, fmt.Errorf("indeterminate: point has unknown start")
			}
			if point.timestamp > from && point.start < to {
				return cumulativeEpoch{}, fmt.Errorf("indeterminate: overlapping cumulative epoch")
			}
			continue
		}
		if point.flags != 0 || (len(epoch.points) > 0 && point.timestamp == epoch.points[len(epoch.points)-1].timestamp) {
			return cumulativeEpoch{}, fmt.Errorf("indeterminate: flagged or duplicate cumulative point")
		}
		if point.timestamp == from {
			epoch.first = len(epoch.points)
		}
		if point.timestamp == to {
			epoch.last = len(epoch.points)
		}
		epoch.points = append(epoch.points, point)
	}
	if epoch.first < 0 || epoch.last < 0 {
		return cumulativeEpoch{}, fmt.Errorf("indeterminate: selected endpoints are missing or reset")
	}
	return epoch, nil
}
