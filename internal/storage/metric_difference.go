package storage

import (
	"bytes"
	"database/sql"
	"fmt"
	"math/big"
	"sort"

	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/encoding/protojson"
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
	metricCandidateByteLimit = 128 << 20
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

func observedMetricPoints(db *sql.DB, selector []byte, from, to uint64) ([]metricObservation, []metricObservation, error) {
	id, err := parseMetricSelection(selector)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid selector: %w", err)
	}
	if from == 0 || to == 0 || from >= to {
		return nil, nil, fmt.Errorf("point timestamps must be nonzero and increasing")
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	pages, err := metricEnvelopePages(tx)
	if err != nil {
		return nil, nil, err
	}
	var points []metricObservation
	var later []metricObservation
	var candidateBytes int
	for _, page := range pages {
		err := visitMetricPage(tx, page, func(raw []byte) error {
			pruned, keep, err := pruneMetricEnvelope(raw, id, &candidateBytes)
			if err != nil {
				return err
			}
			if !keep {
				return nil
			}
			raw = pruned
			data := &metricpb.MetricsData{}
			if err := (protojson.UnmarshalOptions{DiscardUnknown: true, RecursionLimit: metricJSONRecursionLimit}).Unmarshal(raw, data); err != nil {
				return fmt.Errorf("indeterminate: invalid retained metrics envelope: %w", err)
			}
			for ri, resource := range data.ResourceMetrics {
				if resource == nil {
					return fmt.Errorf("indeterminate: missing resource envelope")
				}
				if resource.SchemaUrl != id.resourceSchema {
					continue
				}
				for si, scope := range resource.ScopeMetrics {
					if scope == nil {
						return fmt.Errorf("indeterminate: missing scope envelope")
					}
					if scope.SchemaUrl != id.scopeSchema {
						continue
					}
					if scope.Scope != nil {
						if scope.Scope.Name != id.scopeName || scope.Scope.Version != id.scopeVersion {
							continue
						}
					} else if id.scopeName != "" || id.scopeVersion != "" {
						continue
					}
					for mi, metric := range scope.Metrics {
						if metric == nil {
							return fmt.Errorf("indeterminate: missing metric")
						}
						if metric.Name != id.name || metric.Unit != id.unit {
							continue
						}
						candidateBytes += 2 * len(raw)
						if candidateBytes > metricCandidateByteLimit {
							return fmt.Errorf("indeterminate: metric candidate processing limit exceeded")
						}
						selected, err := selectRawMetric(raw, ri, si, mi, true)
						if err != nil {
							return fmt.Errorf("indeterminate: selecting retained metric: %w", err)
						}
						strict, err := decodeMetricEnvelope(selected)
						if err != nil {
							return fmt.Errorf("indeterminate: unsupported matching metrics envelope: %w", err)
						}
						r := strict.ResourceMetrics[0]
						sc := r.ScopeMetrics[0]
						resourceKey, err := canonicalResource(r.Resource, false)
						if err != nil {
							if r.Resource != nil && knownAttributesDisjoint(r.Resource.Attributes, id.resourceAttrs) {
								continue
							}
							return fmt.Errorf("indeterminate: %w", err)
						}
						if !bytes.Equal(resourceKey, id.resource) {
							continue
						}
						scopeKey, err := canonicalScope(sc.Scope, false)
						if err != nil {
							if sc.Scope != nil && knownAttributesDisjoint(sc.Scope.Attributes, id.scopeAttrs) {
								continue
							}
							return fmt.Errorf("indeterminate: %w", err)
						}
						if !bytes.Equal(scopeKey, id.scope) {
							continue
						}
						selected, err = selectRawMetric(raw, ri, si, mi, false)
						if err != nil {
							return fmt.Errorf("indeterminate: selecting retained metric: %w", err)
						}
						strict, err = decodeMetricEnvelope(selected)
						if err != nil {
							return fmt.Errorf("indeterminate: unsupported matching metric: %w", err)
						}
						m := strict.ResourceMetrics[0].ScopeMetrics[0].Metrics[0]
						sum := m.GetSum()
						if sum == nil || !sum.IsMonotonic || sum.AggregationTemporality != metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE {
							return fmt.Errorf("indeterminate: conflicting metric type or temporality")
						}
						for _, point := range sum.DataPoints {
							if point == nil {
								return fmt.Errorf("indeterminate: missing data point")
							}
							key, err := canonicalPoint(point.Attributes, false)
							if err != nil {
								return fmt.Errorf("indeterminate: %w", err)
							}
							if !bytes.Equal(key, id.attributes) {
								continue
							}
							if point.TimeUnixNano == 0 {
								return fmt.Errorf("indeterminate: matching point has no timestamp")
							}
							if point.StartTimeUnixNano > point.TimeUnixNano {
								return fmt.Errorf("indeterminate: point has invalid start time")
							}
							if point.TimeUnixNano < from {
								continue
							}
							if point.TimeUnixNano > to {
								if point.StartTimeUnixNano == 0 {
									return fmt.Errorf("indeterminate: later point has unknown start")
								}
								if point.StartTimeUnixNano < to {
									value, ok := point.Value.(*metricpb.NumberDataPoint_AsInt)
									observation := metricObservation{timestamp: point.TimeUnixNano, start: point.StartTimeUnixNano, flags: point.Flags, integer: ok}
									if ok {
										observation.value = value.AsInt
									}
									later = append(later, observation)
								}
							} else {
								value, ok := point.Value.(*metricpb.NumberDataPoint_AsInt)
								observation := metricObservation{timestamp: point.TimeUnixNano, start: point.StartTimeUnixNano, flags: point.Flags, integer: ok}
								if ok {
									observation.value = value.AsInt
								}
								points = append(points, observation)
							}
							if len(points)+len(later) > metricPointLimit {
								return fmt.Errorf("indeterminate: matching point scan limit exceeded")
							}
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}

	return points, later, nil
}

func QueryMetricDifference(db *sql.DB, selector []byte, from, to uint64) (MetricDifference, error) {
	result := MetricDifference{}
	points, later, err := observedMetricPoints(db, selector, from, to)
	if err != nil {
		return result, err
	}
	sort.Slice(points, func(i, j int) bool { return points[i].timestamp < points[j].timestamp })
	if len(points) < 2 || points[0].timestamp != from || points[len(points)-1].timestamp != to {
		return result, fmt.Errorf("indeterminate: selected endpoint points are missing")
	}
	var start uint64
	var previous int64
	for i, point := range points {
		if !point.integer || point.flags != 0 || point.start == 0 || point.start > point.timestamp {
			return result, fmt.Errorf("indeterminate: point has invalid value, flags, or start")
		}
		if i == 0 {
			start = point.start
		} else if point.timestamp == points[i-1].timestamp || point.start != start || point.value < previous {
			return result, fmt.Errorf("indeterminate: duplicate point, reset, or decreasing value")
		}
		previous = point.value
	}
	sort.Slice(later, func(i, j int) bool { return later[i].timestamp < later[j].timestamp })
	for i, point := range later {
		if point.start != start {
			return result, fmt.Errorf("indeterminate: later point declares an overlapping cumulative epoch")
		}
		if !point.integer || point.flags != 0 || point.value < previous || (i > 0 && point.timestamp == later[i-1].timestamp) {
			return result, fmt.Errorf("indeterminate: later point conflicts with the cumulative epoch")
		}
		previous = point.value
	}
	difference := new(big.Int).Sub(big.NewInt(points[len(points)-1].value), big.NewInt(points[0].value))
	result = MetricDifference{FromUnixNano: fmt.Sprint(from), ToUnixNano: fmt.Sprint(to), Difference: difference.String(), ElapsedNano: fmt.Sprint(to - from), Assumptions: []string{"Observed retained points only; ingestion or retention may be incomplete.", "The selected stream is assumed to have a single writer."}}
	return result, nil
}
