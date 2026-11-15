package storage

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"sort"

	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

type MetricDeltaTotal struct {
	FromUnixNano  string   `json:"from_unix_nano"`
	ToUnixNano    string   `json:"to_unix_nano"`
	Total         string   `json:"total"`
	RatePerSecond string   `json:"rate_per_second"`
	PointCount    int      `json:"point_count"`
	Assumptions   []string `json:"assumptions"`
}

func QueryMetricDeltaTotal(ctx context.Context, db *sql.DB, selector MetricSelector, from, to uint64) (MetricDeltaTotal, error) {
	result := MetricDeltaTotal{}
	if selector.kind != metricSum || selector.temporality != metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA {
		return result, fmt.Errorf("invalid delta metric selector")
	}
	points, err := observedMetricPoints(ctx, db, selector, from, to)
	if err != nil {
		return result, err
	}
	total := new(big.Int)
	count, err := visitDeltaInterval(ctx, points, from, to, func(point metricObservation) error {
		if !point.integer || point.value < 0 {
			return fmt.Errorf("indeterminate: invalid delta integer value")
		}
		total.Add(total, big.NewInt(point.value))
		return nil
	})
	if err != nil {
		return result, err
	}
	return MetricDeltaTotal{
		FromUnixNano: fmt.Sprint(from), ToUnixNano: fmt.Sprint(to), Total: total.String(), PointCount: count,
		RatePerSecond: observedRate(total, to-from), Assumptions: metricAssumptions(),
	}, nil
}

func visitDeltaInterval(ctx context.Context, points []metricObservation, from, to uint64, visit func(metricObservation) error) (int, error) {
	sort.Slice(points, func(i, j int) bool { return points[i].timestamp < points[j].timestamp })
	cursor, count := from, 0
	for _, point := range points {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if point.timestamp <= from {
			continue
		}
		if point.start == 0 {
			return 0, fmt.Errorf("indeterminate: point has unknown start")
		}
		if point.start >= to && point.timestamp > to {
			continue
		}
		if point.start >= point.timestamp || point.start != cursor || point.timestamp > to || point.flags != 0 {
			return 0, fmt.Errorf("indeterminate: gap, overlap or invalid delta point")
		}
		if err := visit(point); err != nil {
			return 0, err
		}
		count++
		cursor = point.timestamp
	}
	if cursor != to {
		return 0, fmt.Errorf("indeterminate: selected interval is not fully covered")
	}
	return count, nil
}
