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
	FromUnixNano string   `json:"from_unix_nano"`
	ToUnixNano   string   `json:"to_unix_nano"`
	Total        string   `json:"total"`
	PointCount   int      `json:"point_count"`
	Assumptions  []string `json:"assumptions"`
}

func QueryMetricDeltaTotal(ctx context.Context, db *sql.DB, selector MetricSelector, from, to uint64) (MetricDeltaTotal, error) {
	result := MetricDeltaTotal{}
	if selector.temporality != metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA {
		return result, fmt.Errorf("invalid delta metric selector")
	}
	points, err := observedMetricPoints(ctx, db, selector, from, to)
	if err != nil {
		return result, err
	}
	sort.Slice(points, func(i, j int) bool { return points[i].timestamp < points[j].timestamp })
	cursor := from
	total := new(big.Int)
	count := 0
	for _, point := range points {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if point.timestamp <= from {
			continue
		}
		if point.start == 0 {
			return result, fmt.Errorf("indeterminate: point has unknown start")
		}
		if point.start >= to && point.timestamp > to {
			continue
		}
		if point.start >= point.timestamp || point.start != cursor || point.timestamp > to || !point.integer || point.flags != 0 || point.value < 0 {
			return result, fmt.Errorf("indeterminate: gap, overlap or invalid delta point")
		}
		total.Add(total, big.NewInt(point.value))
		count++
		cursor = point.timestamp
	}
	if cursor != to {
		return result, fmt.Errorf("indeterminate: selected interval is not fully covered")
	}
	return MetricDeltaTotal{FromUnixNano: fmt.Sprint(from), ToUnixNano: fmt.Sprint(to), Total: total.String(), PointCount: count, Assumptions: []string{"Observed retained points only; ingestion or retention may be incomplete.", "The selected stream is assumed to have a single writer."}}, nil
}
