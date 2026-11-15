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
