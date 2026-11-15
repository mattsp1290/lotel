package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func metricEnvelope(scope, points string) string {
	return fmt.Sprintf(`{"resourceMetrics":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"app"}},{"key":"region","value":{"intValue":"7"}}]},"schemaUrl":"resource-schema","scopeMetrics":[{"scope":{"name":"sdk","version":"1","attributes":[{"key":"tenant","value":{"stringValue":%q}}]},"schemaUrl":"scope-schema","metrics":[{"name":"requests","unit":"1","sum":{"aggregationTemporality":2,"isMonotonic":true,"dataPoints":[%s]}}]}]}]}`, scope, points)
}
func differenceTestPoint(t, start, value string) string {
	return fmt.Sprintf(`{"attributes":[{"key":"method","value":{"stringValue":"GET"}}],"timeUnixNano":%q,"startTimeUnixNano":%q,"asInt":%q}`, t, start, value)
}
func ingestMetricsForDifference(t *testing.T, envelopes ...string) *sql.DB {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "metrics")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metrics.jsonl"), []byte(strings.Join(envelopes, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = IngestAll(db, root); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMetricDifferenceExactRetainedPoints(t *testing.T) {
	first := differenceTestPoint("1700000000000000001", "1600000000000000000", "9007199254740993")
	middle := differenceTestPoint("1700000000000000002", "1600000000000000000", "9007199254740995")
	last := differenceTestPoint("1700000000000000003", "1600000000000000000", "9007199254741000")
	selector := metricEnvelope("A", first)
	db := ingestMetricsForDifference(t, metricEnvelope("B", first), metricEnvelope("A", first+","+middle+","+last))
	got, err := QueryMetricDifference(db, []byte(selector), 1700000000000000001, 1700000000000000003)
	if err != nil {
		t.Fatal(err)
	}
	if got.Difference != "7" || got.ElapsedNano != "2" || len(got.Assumptions) != 2 {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestMetricDifferenceRejectsUncertainIntervals(t *testing.T) {
	first := differenceTestPoint("1700000000000000001", "1600000000000000000", "9007199254740993")
	last := differenceTestPoint("1700000000000000003", "1600000000000000000", "9007199254741000")
	middle := differenceTestPoint("1700000000000000002", "1600000000000000000", "9007199254740995")
	cases := []struct{ name, points string }{
		{"missing endpoint", first},
		{"undated matching point", first + `,{"attributes":[{"key":"method","value":{"stringValue":"GET"}}],"asInt":"12"},` + last},
		{"duplicate", first + "," + first + "," + last},
		{"reset", first + "," + strings.Replace(middle, "1600000000000000000", "1600000000000000001", 1) + "," + last},
		{"decrease", first + "," + strings.Replace(middle, "9007199254740995", "1", 1) + "," + last},
		{"missing start", first + "," + strings.Replace(middle, `"startTimeUnixNano":"1600000000000000000",`, "", 1) + "," + last},
		{"flagged", first + "," + strings.Replace(middle, `"asInt":`, `"flags":1,"asInt":`, 1) + "," + last},
		{"mixed value", first + "," + strings.Replace(middle, `"asInt":"9007199254740995"`, `"asDouble":1.5`, 1) + "," + last},
	}
	selector := metricEnvelope("A", first)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := ingestMetricsForDifference(t, metricEnvelope("A", tc.points))
			if _, err := QueryMetricDifference(db, []byte(selector), 1700000000000000001, 1700000000000000003); err == nil {
				t.Fatal("expected indeterminate result")
			}
		})
	}
}

func TestMetricDifferenceRejectsAmbiguousSelector(t *testing.T) {
	point := differenceTestPoint("1700000000000000001", "1600000000000000000", "1")
	selector := metricEnvelope("A", point)
	selector = strings.Replace(selector, `{"key":"tenant","value":{"stringValue":"A"}}`, `{"key":"tenant","value":{"stringValue":"A"}},{"key":"tenant","value":{"stringValue":"B"}}`, 1)
	if err := ValidateMetricSelector([]byte(selector)); err == nil {
		t.Fatal("duplicate scope attribute accepted")
	}
}

func TestMetricDifferenceUsesFullTypedIdentity(t *testing.T) {
	first := differenceTestPoint("1700000000000000001", "1600000000000000000", "10")
	last := differenceTestPoint("1700000000000000003", "1600000000000000000", "20")
	selector := metricEnvelope("A", first)
	otherScope := metricEnvelope("B", differenceTestPoint("1700000000000000002", "1", "0"))
	otherType := strings.Replace(metricEnvelope("A", differenceTestPoint("1700000000000000002", "1", "0")), `"intValue":"7"`, `"stringValue":"7"`, 1)
	reordered := strings.Replace(metricEnvelope("A", first+","+last), `{"key":"service.name","value":{"stringValue":"app"}},{"key":"region","value":{"intValue":"7"}}`, `{"key":"region","value":{"intValue":"7"}},{"key":"service.name","value":{"stringValue":"app"}}`, 1)
	db := ingestMetricsForDifference(t, otherScope, otherType, reordered)
	got, err := QueryMetricDifference(db, []byte(selector), 1700000000000000001, 1700000000000000003)
	if err != nil || got.Difference != "10" {
		t.Fatalf("identity mismatch: %+v %v", got, err)
	}
}

func TestMetricDifferenceNormalizesSignedZeroIdentity(t *testing.T) {
	first := strings.Replace(differenceTestPoint("1700000000000000001", "1600000000000000000", "10"), `"stringValue":"GET"`, `"doubleValue":-0.0`, 1)
	last := strings.Replace(differenceTestPoint("1700000000000000003", "1600000000000000000", "20"), `"stringValue":"GET"`, `"doubleValue":0.0`, 1)
	duplicate := strings.Replace(first, `"doubleValue":-0.0`, `"doubleValue":0.0`, 1)
	selector := metricEnvelope("A", first)
	db := ingestMetricsForDifference(t, metricEnvelope("A", first+","+duplicate+","+last))
	if _, err := QueryMetricDifference(db, []byte(selector), 1700000000000000001, 1700000000000000003); err == nil {
		t.Fatal("duplicate point with signed-zero attribute accepted")
	}
}

func TestMetricDifferencePreservesNumericJSONIntegers(t *testing.T) {
	first := differenceTestPoint("1700000000000000001", "1600000000000000000", "9007199254740993")
	last := differenceTestPoint("1700000000000000002", "1600000000000000000", "9007199254741000")
	selector := metricEnvelope("A", first)
	data := metricEnvelope("A", first+","+last)
	for _, n := range []string{"1700000000000000001", "1700000000000000002", "1600000000000000000", "9007199254740993", "9007199254741000"} {
		data = strings.ReplaceAll(data, `"`+n+`"`, n)
	}
	db := ingestMetricsForDifference(t, data)
	got, err := QueryMetricDifference(db, []byte(selector), 1700000000000000001, 1700000000000000002)
	if err != nil || got.Difference != "7" || got.ElapsedNano != "1" {
		t.Fatalf("lost numeric precision: %+v %v", got, err)
	}
}

func TestMetricDifferenceRequiresRetainedEvidenceAndCanBackfill(t *testing.T) {
	first := differenceTestPoint("1700000000000000001", "1600000000000000000", "10")
	last := differenceTestPoint("1700000000000000002", "1600000000000000000", "20")
	selector := metricEnvelope("A", first)
	root := t.TempDir()
	dir := filepath.Join(root, "metrics")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metrics.jsonl"), []byte(metricEnvelope("A", first+","+last)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := IngestAll(db, root); err != nil {
		t.Fatal(err)
	}
	if _, err := Prune(db, time.Unix(1800000000, 0), "app", false); err != nil {
		t.Fatal(err)
	}
	if _, err := QueryMetricDifference(db, []byte(selector), 1700000000000000001, 1700000000000000002); err == nil {
		t.Fatal("pruned endpoints accepted")
	}
	if _, err := ReingestAll(db, root); err != nil {
		t.Fatal(err)
	}
	got, err := QueryMetricDifference(db, []byte(selector), 1700000000000000001, 1700000000000000002)
	if err != nil || got.Difference != "10" {
		t.Fatalf("backfill: %+v %v", got, err)
	}
}

func TestMetricDifferenceCanExceedSignedIntegerRange(t *testing.T) {
	first := differenceTestPoint("1700000000000000001", "1600000000000000000", "-9223372036854775808")
	last := differenceTestPoint("1700000000000000002", "1600000000000000000", "9223372036854775807")
	db := ingestMetricsForDifference(t, metricEnvelope("A", first+","+last))
	got, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 1700000000000000001, 1700000000000000002)
	if err != nil || got.Difference != "18446744073709551615" {
		t.Fatalf("overflow: %+v %v", got, err)
	}
}

func TestMetricDifferenceSkipsUnrelatedNonfiniteAttributes(t *testing.T) {
	first := differenceTestPoint("1700000000000000001", "1600000000000000000", "10")
	last := differenceTestPoint("1700000000000000002", "1600000000000000000", "20")
	selector := metricEnvelope("A", first)
	unrelated := strings.Replace(metricEnvelope("A", first), `"intValue":"7"`, `"doubleValue":"NaN"`, 1)
	db := ingestMetricsForDifference(t, unrelated, metricEnvelope("A", first+","+last))
	got, err := QueryMetricDifference(db, []byte(selector), 1700000000000000001, 1700000000000000002)
	if err != nil || got.Difference != "10" {
		t.Fatalf("unrelated nonfinite attribute: %+v %v", got, err)
	}
	if err := ValidateMetricSelector([]byte(unrelated)); err == nil {
		t.Fatal("nonfinite selector accepted")
	}
}

func TestMetricDifferenceRejectsLaterOverlappingEpoch(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	overlap := differenceTestPoint("40", "20", "3")
	db := ingestMetricsForDifference(t, metricEnvelope("A", first+","+last+","+overlap))
	if _, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30); err == nil {
		t.Fatal("later point with overlapping epoch accepted")
	}
}

func TestMetricDifferenceIgnoresProvablyDisjointIncompleteResources(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	selected := metricEnvelope("A", first+","+last)
	other := strings.Replace(metricEnvelope("A", first), `"stringValue":"app"`, `"stringValue":"other"`, 1)
	other = strings.Replace(other, `"resource":{"attributes":`, `"resource":{"droppedAttributesCount":1,"attributes":`, 1)
	db := ingestMetricsForDifference(t, other, selected)
	got, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30)
	if err != nil || got.Difference != "10" {
		t.Fatalf("disjoint incomplete resource: %+v %v", got, err)
	}
}

func TestMetricDifferenceRejectsPossiblyMatchingIncompleteResources(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	selected := metricEnvelope("A", first+","+last)
	uncertain := strings.Replace(metricEnvelope("A", first), `,{"key":"region","value":{"intValue":"7"}}`, "", 1)
	uncertain = strings.Replace(uncertain, `"resource":{"attributes":`, `"resource":{"droppedAttributesCount":1,"attributes":`, 1)
	db := ingestMetricsForDifference(t, uncertain, selected)
	if _, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30); err == nil {
		t.Fatal("possibly matching incomplete resource accepted")
	}
}

func TestMetricDifferenceIgnoresUnknownFieldsInUnrelatedMetric(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	selected := metricEnvelope("A", first+","+last)
	other := strings.Replace(metricEnvelope("A", first), `"name":"requests"`, `"name":"other","newField":42`, 1)
	db := ingestMetricsForDifference(t, other, selected)
	got, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30)
	if err != nil || got.Difference != "10" {
		t.Fatalf("unrelated metric: %+v %v", got, err)
	}
}

func TestMetricDifferenceRejectsUnknownFieldsInPossiblyMatchingMetric(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	unknown := strings.Replace(metricEnvelope("A", first), `"name":"requests"`, `"name":"requests","newField":42`, 1)
	db := ingestMetricsForDifference(t, unknown, metricEnvelope("A", first+","+last))
	if _, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30); err == nil {
		t.Fatal("unknown field in matching metric accepted")
	}
}

func TestMetricDifferenceAcceptsNonoverlappingLaterEpoch(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	for _, tc := range []struct{ name, later string }{
		{"same epoch", differenceTestPoint("40", "2", "20")},
		{"later epoch", differenceTestPoint("40", "31", "1")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := ingestMetricsForDifference(t, metricEnvelope("A", first+","+last+","+tc.later))
			got, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30)
			if err != nil || got.Difference != "10" {
				t.Fatalf("later point: %+v %v", got, err)
			}
		})
	}
}

func TestMetricDifferenceRejectsLaterUnknownEpoch(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	later := strings.Replace(differenceTestPoint("40", "2", "20"), `"startTimeUnixNano":"2",`, "", 1)
	db := ingestMetricsForDifference(t, metricEnvelope("A", first+","+last+","+later))
	if _, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30); err == nil {
		t.Fatal("later point with unknown start accepted")
	}
}

func TestMetricDifferenceIgnoresUnknownSiblingMetric(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	data := strings.Replace(metricEnvelope("A", first+","+last), `"metrics":[`, `"metrics":[{"name":"other","newField":42},`, 1)
	db := ingestMetricsForDifference(t, data)
	got, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30)
	if err != nil || got.Difference != "10" {
		t.Fatalf("unrelated sibling metric: %+v %v", got, err)
	}
}

func TestMetricDifferenceHandlesIncompleteScopes(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	selected := metricEnvelope("A", first+","+last)
	disjoint := strings.Replace(metricEnvelope("A", first), `"scope":{"name":"sdk","version":"1","attributes":`, `"scope":{"name":"sdk","version":"1","droppedAttributesCount":1,"attributes":`, 1)
	disjoint = strings.Replace(disjoint, `"stringValue":"A"`, `"stringValue":"B"`, 1)
	t.Run("disjoint", func(t *testing.T) {
		db := ingestMetricsForDifference(t, disjoint, selected)
		got, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30)
		if err != nil || got.Difference != "10" {
			t.Fatalf("disjoint scope: %+v %v", got, err)
		}
	})
	uncertain := strings.Replace(metricEnvelope("A", first), `"scope":{"name":"sdk","version":"1","attributes":[{"key":"tenant","value":{"stringValue":"A"}}]}`, `"scope":{"name":"sdk","version":"1","droppedAttributesCount":1}`, 1)
	t.Run("possibly matching", func(t *testing.T) {
		db := ingestMetricsForDifference(t, uncertain, selected)
		if _, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30); err == nil {
			t.Fatal("possibly matching incomplete scope accepted")
		}
	})
}

func TestMetricDifferenceRejectsOversizedEnvelope(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	oversized := `{"resourceMetrics":[],"padding":"` + strings.Repeat("x", metricEnvelopeCharLimit) + `"}`
	if _, err := db.Exec(`INSERT INTO otlp_envelopes (signal,service_name,payload) VALUES ('metrics','other',?)`, oversized); err != nil {
		t.Fatal(err)
	}
	selector := metricEnvelope("A", differenceTestPoint("10", "2", "7"))
	if _, err := QueryMetricDifference(db, []byte(selector), 10, 30); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized envelope: %v", err)
	}
}

func TestMetricDifferenceIgnoresUnknownMetricInDisjointIncompleteResource(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	other := strings.Replace(metricEnvelope("A", first), `"stringValue":"app"`, `"stringValue":"other"`, 1)
	other = strings.Replace(other, `"resource":{"attributes":`, `"resource":{"droppedAttributesCount":1,"attributes":`, 1)
	other = strings.Replace(other, `"name":"requests"`, `"name":"requests","newField":42`, 1)
	db := ingestMetricsForDifference(t, other, metricEnvelope("A", first+","+last))
	got, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30)
	if err != nil || got.Difference != "10" {
		t.Fatalf("disjoint resource with unknown metric: %+v %v", got, err)
	}
}

func TestMetricDifferenceRejectsInvalidEarlierInterval(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	invalid := differenceTestPoint("9", "20", "1")
	db := ingestMetricsForDifference(t, metricEnvelope("A", first+","+last+","+invalid))
	if _, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30); err == nil {
		t.Fatal("invalid earlier interval accepted")
	}
}
