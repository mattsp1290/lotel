package storage

import (
	"context"
	"strings"
	"testing"
)

func deltaEnvelope(scope, points string) string {
	return strings.Replace(metricEnvelope(scope, points), `"aggregationTemporality":2`, `"aggregationTemporality":1`, 1)
}

func TestMetricDeltaTotalContiguousExactCoverage(t *testing.T) {
	a := differenceTestPoint("20", "10", "9223372036854775807")
	b := differenceTestPoint("30", "20", "2")
	other := differenceTestPoint("40", "30", "100")
	db := ingestMetricsForDifference(t, deltaEnvelope("A", b+","+other+","+a), deltaEnvelope("B", a))
	selector, err := PrepareDeltaMetricSelector(context.Background(), []byte(deltaEnvelope("A", a)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := QueryMetricDeltaTotal(context.Background(), db, selector, 10, 30)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != "9223372036854775809" || got.RatePerSecond != "461168601842738790450000000" || got.PointCount != 2 || got.FromUnixNano != "10" || got.ToUnixNano != "30" || len(got.Assumptions) != 2 {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestMetricDeltaTotalRejectsUncertainCoverage(t *testing.T) {
	a := differenceTestPoint("20", "10", "1")
	b := differenceTestPoint("30", "20", "2")
	cases := []struct{ name, envelope string }{
		{"gap", deltaEnvelope("A", a+","+differenceTestPoint("30", "21", "2"))},
		{"overlap", deltaEnvelope("A", a+","+differenceTestPoint("30", "19", "2"))},
		{"duplicate", deltaEnvelope("A", a+","+a+","+b)},
		{"missing end", deltaEnvelope("A", a)},
		{"missing start", deltaEnvelope("A", strings.Replace(a, `"startTimeUnixNano":"10",`, "", 1)+","+b)},
		{"crosses start", deltaEnvelope("A", differenceTestPoint("20", "9", "1")+","+b)},
		{"crosses end", deltaEnvelope("A", a+","+differenceTestPoint("31", "20", "2"))},
		{"zero length reset", deltaEnvelope("A", a+","+differenceTestPoint("20", "20", "0")+","+b)},
		{"flagged", deltaEnvelope("A", strings.Replace(a, `"asInt":`, `"flags":1,"asInt":`, 1)+","+b)},
		{"float", deltaEnvelope("A", strings.Replace(a, `"asInt":"1"`, `"asDouble":1`, 1)+","+b)},
		{"negative", deltaEnvelope("A", differenceTestPoint("20", "10", "-1")+","+b)},
		{"invalid point", deltaEnvelope("A", differenceTestPoint("20", "21", "1")+","+b)},
		{"conflicting cumulative", deltaEnvelope("A", a+","+b) + "\n" + metricEnvelope("A", a)},
		{"ambiguous resource", strings.Replace(deltaEnvelope("A", a+","+b), `"resource":{`, `"resource":{"droppedAttributesCount":1,`, 1)},
		{"malformed envelope", deltaEnvelope("A", a+","+b) + "\n" + `{"resourceMetrics":[{"bad":1}]}`},
	}
	selector, err := PrepareDeltaMetricSelector(context.Background(), []byte(deltaEnvelope("A", a)))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := ingestMetricsForDifference(t, tc.envelope)
			if got, err := QueryMetricDeltaTotal(context.Background(), db, selector, 10, 30); err == nil {
				t.Fatalf("accepted uncertain interval: %+v", got)
			}
		})
	}
}

func TestMetricDeltaTotalChecksSelectorTemporality(t *testing.T) {
	a := differenceTestPoint("20", "10", "1")
	db := ingestMetricsForDifference(t, deltaEnvelope("A", a))
	if _, err := PrepareDeltaMetricSelector(context.Background(), []byte(metricEnvelope("A", a))); err == nil {
		t.Fatal("accepted cumulative selector")
	}
	selector, err := PrepareMetricSelector(context.Background(), []byte(metricEnvelope("A", a)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = QueryMetricDeltaTotal(context.Background(), db, selector, 10, 20); err == nil {
		t.Fatal("accepted wrong selector type")
	}
}

func TestMetricDeltaTotalAllowsResetAtExcludedStart(t *testing.T) {
	marker := differenceTestPoint("10", "10", "5")
	first := differenceTestPoint("20", "10", "3")
	second := differenceTestPoint("30", "20", "4")
	db := ingestMetricsForDifference(t, deltaEnvelope("A", marker+","+first+","+second))
	selector, err := PrepareDeltaMetricSelector(context.Background(), []byte(deltaEnvelope("A", first)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := QueryMetricDeltaTotal(context.Background(), db, selector, 10, 30)
	if err != nil || got.Total != "7" || got.PointCount != 2 {
		t.Fatalf("excluded reset should not affect interval: %+v, %v", got, err)
	}
}
