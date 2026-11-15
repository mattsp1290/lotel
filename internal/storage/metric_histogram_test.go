package storage

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func histogramEnvelope(scope, points string) string {
	return strings.Replace(metricEnvelope(scope, points), `"sum":{"aggregationTemporality":2,"isMonotonic":true`, `"histogram":{"aggregationTemporality":1`, 1)
}

func histogramPoint(end, start, count, buckets, bounds string) string {
	return fmt.Sprintf(`{"attributes":[{"key":"method","value":{"stringValue":"GET"}}],"timeUnixNano":%q,"startTimeUnixNano":%q,"count":%q,"bucketCounts":[%s],"explicitBounds":[%s]}`, end, start, count, buckets, bounds)
}

func TestMetricDeltaHistogramExactCounts(t *testing.T) {
	a := histogramPoint("20", "10", "18446744073709551615", `"18446744073709551614","1"`, "1")
	b := histogramPoint("30", "20", "2", `"1","1"`, "1")
	marker := histogramPoint("10", "10", "0", `"0","0"`, "1")
	db := ingestMetricsForDifference(t, histogramEnvelope("A", b+","+marker+","+a), histogramEnvelope("B", a))
	selector, err := PrepareDeltaHistogramSelector(context.Background(), []byte(histogramEnvelope("A", a)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := QueryMetricHistogram(context.Background(), db, selector, 10, 30)
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != "18446744073709551617" || got.PointCount != 2 || len(got.Bounds) != 1 || got.Bounds[0] != 1 || len(got.BucketCounts) != 2 || got.BucketCounts[0] != "18446744073709551615" || got.BucketCounts[1] != "2" {
		t.Fatalf("unexpected summary: %+v", got)
	}
}

func TestMetricDeltaHistogramRejectsUncertainEvidence(t *testing.T) {
	a := histogramPoint("20", "10", "2", `"1","1"`, "1")
	b := histogramPoint("30", "20", "2", `"1","1"`, "1")
	cases := []struct{ name, data string }{
		{"gap", histogramEnvelope("A", a+","+histogramPoint("30", "21", "2", `"1","1"`, "1"))},
		{"overlap", histogramEnvelope("A", a+","+histogramPoint("30", "19", "2", `"1","1"`, "1"))},
		{"duplicate", histogramEnvelope("A", a+","+a+","+b)},
		{"reset", histogramEnvelope("A", a+","+histogramPoint("20", "20", "0", `"0","0"`, "1")+","+b)},
		{"missing start", histogramEnvelope("A", strings.Replace(a, `"startTimeUnixNano":"10",`, "", 1)+","+b)},
		{"crosses end", histogramEnvelope("A", a+","+histogramPoint("31", "20", "2", `"1","1"`, "1"))},
		{"flags", histogramEnvelope("A", strings.Replace(a, `"count":`, `"flags":1,"count":`, 1)+","+b)},
		{"missing buckets", histogramEnvelope("A", strings.Replace(a, `"bucketCounts":["1","1"],`, "", 1)+","+b)},
		{"mismatched count", histogramEnvelope("A", histogramPoint("20", "10", "3", `"1","1"`, "1")+","+b)},
		{"different bounds", histogramEnvelope("A", a+","+histogramPoint("30", "20", "2", `"1","1"`, "2"))},
		{"unordered bounds", histogramEnvelope("A", histogramPoint("20", "10", "2", `"1","0","1"`, "2,1")+","+b)},
		{"infinite bound", histogramEnvelope("A", histogramPoint("20", "10", "2", `"1","1"`, `"Infinity"`)+","+b)},
		{"cumulative", strings.Replace(histogramEnvelope("A", a+","+b), `"aggregationTemporality":1`, `"aggregationTemporality":2`, 1)},
		{"conflicting sum", histogramEnvelope("A", a+","+b) + "\n" + deltaEnvelope("A", differenceTestPoint("20", "10", "1"))},
		{"ambiguous identity", strings.Replace(histogramEnvelope("A", a+","+b), `"resource":{`, `"resource":{"droppedAttributesCount":1,`, 1)},
		{"malformed", histogramEnvelope("A", a+","+b) + "\n" + `{"resourceMetrics":[{"bad":1}]}`},
	}
	selector, err := PrepareDeltaHistogramSelector(context.Background(), []byte(histogramEnvelope("A", a)))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := ingestMetricsForDifference(t, tc.data)
			if got, err := QueryMetricHistogram(context.Background(), db, selector, 10, 30); err == nil {
				t.Fatalf("accepted uncertain histogram: %+v", got)
			}
		})
	}
}

func TestMetricDeltaHistogramSingleBucket(t *testing.T) {
	point := histogramPoint("20", "10", "3", `"3"`, "")
	db := ingestMetricsForDifference(t, histogramEnvelope("A", point))
	selector, err := PrepareDeltaHistogramSelector(context.Background(), []byte(histogramEnvelope("A", point)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := QueryMetricHistogram(context.Background(), db, selector, 10, 20)
	if err != nil || got.Count != "3" || got.Bounds == nil || len(got.Bounds) != 0 || len(got.BucketCounts) != 1 || got.BucketCounts[0] != "3" {
		t.Fatalf("single bucket: %+v %v", got, err)
	}
}

func TestHistogramReportsBucketOnlyScope(t *testing.T) {
	for _, tc := range []struct{ name, sumMinMax, count, buckets, bound string }{
		{"inconsistent metadata", `"sum":3,"min":42,"max":-1,`, "2", `"2","0"`, "5"},
		{"overflowing sum", `"sum":"Infinity","min":1e308,"max":1e308,`, "2", `"2","0"`, "1e308"},
		{"empty histogram with extrema", `"sum":"NaN","min":7,`, "0", `"0","0"`, "5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			point := strings.Replace(histogramPoint("20", "10", tc.count, tc.buckets, tc.bound), `"count":`, tc.sumMinMax+`"count":`, 1)
			data := histogramEnvelope("A", point)
			db := ingestMetricsForDifference(t, data)
			selector, err := PrepareDeltaHistogramSelector(ctx, []byte(data))
			if err != nil {
				t.Fatal(err)
			}
			got, err := QueryMetricHistogram(ctx, db, selector, 10, 20)
			if err != nil || got.Count != tc.count || got.Basis != "contiguous_delta_bucket_counts" || !strings.Contains(got.ValidationScope, "Sum, min and max are ignored") {
				t.Fatalf("bucket-only result: %+v %v", got, err)
			}
		})
	}
}

func cumulativeHistogramEnvelope(scope, points string) string {
	return strings.Replace(histogramEnvelope(scope, points), `"aggregationTemporality":1`, `"aggregationTemporality":2`, 1)
}

func TestMetricCumulativeHistogramReportsObservedIncrease(t *testing.T) {
	first := histogramPoint("10", "10", "9007199254740993", `"9007199254740993","0"`, "5")
	middle := histogramPoint("20", "10", "9007199254740995", `"9007199254740994","1"`, "5")
	last := histogramPoint("30", "10", "9007199254740999", `"9007199254740995","4"`, "5")
	last = strings.Replace(last, `"count":`, `"sum":-100,"min":-7,"max":-6,"count":`, 1)
	db := ingestMetricsForDifference(t, cumulativeHistogramEnvelope("A", last+","+first+","+middle), cumulativeHistogramEnvelope("other", first))
	ctx := context.Background()
	selector, err := PrepareCumulativeHistogramSelector(ctx, []byte(cumulativeHistogramEnvelope("A", first)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := QueryMetricHistogram(ctx, db, selector, 10, 30)
	if err != nil || got.Basis != "cumulative_bucket_count_difference" || got.Count != "6" || got.PointCount != 3 || len(got.BucketCounts) != 2 || got.BucketCounts[0] != "2" || got.BucketCounts[1] != "4" {
		t.Fatalf("observed increase: %+v %v", got, err)
	}
}

func TestMetricCumulativeHistogramRejectsUncertainEpoch(t *testing.T) {
	first := histogramPoint("10", "2", "2", `"0","2"`, "5")
	last := histogramPoint("30", "2", "7", `"3","4"`, "5")
	ctx := context.Background()
	selector, err := PrepareCumulativeHistogramSelector(ctx, []byte(cumulativeHistogramEnvelope("A", first)))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, points string }{
		{"missing endpoint", first},
		{"duplicate endpoint", first + "," + last + "," + last},
		{"decrease inside interval", first + "," + histogramPoint("20", "2", "3", `"2","1"`, "5") + "," + last},
		{"changed bounds", first + "," + histogramPoint("20", "2", "3", `"1","2"`, "6") + "," + last},
		{"reset", first + "," + histogramPoint("20", "19", "3", `"1","2"`, "5") + "," + last},
		{"future overlapping epoch", first + "," + last + "," + histogramPoint("40", "29", "1", `"1","0"`, "5")},
		{"future decrease in selected epoch", first + "," + last + "," + histogramPoint("40", "2", "8", `"5","3"`, "5")},
		{"flagged", first + "," + strings.Replace(last, `"count":`, `"flags":1,"count":`, 1)},
		{"bucket sum overflows uint64", first + "," + histogramPoint("30", "2", "0", `"18446744073709551615","1"`, "5")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := ingestMetricsForDifference(t, cumulativeHistogramEnvelope("A", tc.points))
			if got, err := QueryMetricHistogram(ctx, db, selector, 10, 30); err == nil {
				t.Fatalf("unexpected result: %+v", got)
			}
		})
	}
}
