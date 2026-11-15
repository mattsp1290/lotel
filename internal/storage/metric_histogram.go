package storage

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"math/big"
	"slices"

	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

type MetricHistogram struct {
	FromUnixNano    string    `json:"from_unix_nano"`
	ToUnixNano      string    `json:"to_unix_nano"`
	Basis           string    `json:"basis"`
	ValidationScope string    `json:"validation_scope"`
	Count           string    `json:"count"`
	Bounds          []float64 `json:"bounds"`
	BucketCounts    []string  `json:"bucket_counts"`
	PointCount      int       `json:"point_count"`
	Assumptions     []string  `json:"assumptions"`
}

func QueryMetricHistogram(ctx context.Context, db *sql.DB, selector MetricSelector, from, to uint64) (MetricHistogram, error) {
	if selector.kind != metricHistogram {
		return MetricHistogram{}, fmt.Errorf("invalid histogram selector")
	}
	points, err := observedMetricPoints(ctx, db, selector, from, to)
	if err != nil {
		return MetricHistogram{}, err
	}
	var buckets []big.Int
	var bounds []float64
	var basis string
	var count int
	switch selector.temporality {
	case metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA:
		basis = "contiguous_delta_bucket_counts"
		count, err = visitDeltaInterval(ctx, points, from, to, func(point metricObservation) error {
			hist := point.histogram
			if err := validateReportedBuckets(hist); err != nil {
				return err
			}
			if buckets == nil {
				bounds = hist.ExplicitBounds
				buckets = make([]big.Int, len(hist.BucketCounts))
			} else if !slices.Equal(bounds, hist.ExplicitBounds) {
				return fmt.Errorf("indeterminate: histogram boundaries differ")
			}
			var addend big.Int
			for i, n := range hist.BucketCounts {
				buckets[i].Add(&buckets[i], addend.SetUint64(n))
			}
			return nil
		})
		if err != nil {
			return MetricHistogram{}, err
		}
	case metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE:
		basis = "cumulative_bucket_count_difference"
		epoch, err := observedCumulativeEpoch(ctx, points, from, to)
		if err != nil {
			return MetricHistogram{}, err
		}
		bounds = epoch.points[epoch.first].histogram.ExplicitBounds
		var previous []uint64
		for _, point := range epoch.points {
			if err := ctx.Err(); err != nil {
				return MetricHistogram{}, err
			}
			hist := point.histogram
			if err := validateReportedBuckets(hist); err != nil {
				return MetricHistogram{}, err
			}
			if !slices.Equal(bounds, hist.ExplicitBounds) {
				return MetricHistogram{}, fmt.Errorf("indeterminate: histogram boundaries differ")
			}
			if previous != nil {
				for i, n := range hist.BucketCounts {
					if n < previous[i] {
						return MetricHistogram{}, fmt.Errorf("indeterminate: decreasing cumulative histogram bucket")
					}
				}
			}
			previous = hist.BucketCounts
		}
		first := epoch.points[epoch.first].histogram.BucketCounts
		last := epoch.points[epoch.last].histogram.BucketCounts
		buckets = make([]big.Int, len(first))
		for i, n := range last {
			buckets[i].SetUint64(n - first[i])
		}
		count = epoch.last - epoch.first + 1
	default:
		return MetricHistogram{}, fmt.Errorf("unsupported histogram temporality")
	}
	var total big.Int
	bucketCounts := make([]string, len(buckets))
	for i := range buckets {
		total.Add(&total, &buckets[i])
		bucketCounts[i] = buckets[i].String()
	}
	return MetricHistogram{
		FromUnixNano: fmt.Sprint(from), ToUnixNano: fmt.Sprint(to), Basis: basis,
		ValidationScope: "Reported counts, explicit bounds and bucket counts only. Sum, min and max are ignored.",
		Bounds:          append([]float64{}, bounds...), BucketCounts: bucketCounts, Count: total.String(),
		PointCount: count, Assumptions: metricAssumptions(),
	}, nil
}

func validateReportedBuckets(hist *metricpb.HistogramDataPoint) error {
	if len(hist.BucketCounts) != len(hist.ExplicitBounds)+1 {
		return fmt.Errorf("indeterminate: histogram buckets missing or invalid")
	}
	for i, bound := range hist.ExplicitBounds {
		if math.IsNaN(bound) || math.IsInf(bound, 0) || (i > 0 && bound <= hist.ExplicitBounds[i-1]) {
			return fmt.Errorf("indeterminate: invalid histogram boundaries")
		}
	}
	remaining := hist.Count
	for _, n := range hist.BucketCounts {
		if n > remaining {
			return fmt.Errorf("indeterminate: histogram count differs from buckets")
		}
		remaining -= n
	}
	if remaining != 0 {
		return fmt.Errorf("indeterminate: histogram count differs from buckets")
	}
	return nil
}
