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

func TestMetricDifferenceRejectsUnknownFieldsInUnrelatedMetric(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	selected := metricEnvelope("A", first+","+last)
	other := strings.Replace(metricEnvelope("A", first), `"name":"requests"`, `"name":"other","newField":42`, 1)
	db := ingestMetricsForDifference(t, other, selected)
	got, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30)
	if err == nil {
		t.Fatalf("unrelated metric accepted: %+v", got)
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
		{"adjacent epoch", differenceTestPoint("40", "30", "1")},
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

func TestMetricDifferenceRejectsUnknownSiblingMetric(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	data := strings.Replace(metricEnvelope("A", first+","+last), `"metrics":[`, `"metrics":[{"name":"other","newField":42},`, 1)
	db := ingestMetricsForDifference(t, data)
	got, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30)
	if err == nil {
		t.Fatalf("unrelated sibling metric accepted: %+v", got)
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
	oversized := `{"resourceMetrics":[],"padding":"` + strings.Repeat("x", metricEnvelopeByteLimit) + `"}`
	if _, err := db.Exec(`INSERT INTO otlp_envelopes (signal,service_name,payload) VALUES ('metrics','other',?)`, oversized); err != nil {
		t.Fatal(err)
	}
	selector := metricEnvelope("A", differenceTestPoint("10", "2", "7"))
	if _, err := QueryMetricDifference(db, []byte(selector), 10, 30); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized envelope: %v", err)
	}
}

func TestMetricDifferenceRejectsUnknownMetricInDisjointIncompleteResource(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	other := strings.Replace(metricEnvelope("A", first), `"stringValue":"app"`, `"stringValue":"other"`, 1)
	other = strings.Replace(other, `"resource":{"attributes":`, `"resource":{"droppedAttributesCount":1,"attributes":`, 1)
	other = strings.Replace(other, `"name":"requests"`, `"name":"requests","newField":42`, 1)
	db := ingestMetricsForDifference(t, other, metricEnvelope("A", first+","+last))
	got, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30)
	if err == nil {
		t.Fatalf("disjoint resource with unknown metric accepted: %+v", got)
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

func TestMetricDifferencePreflightsAllEnvelopeSizes(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	malformed := `{"resourceMetrics":"not-an-array"}`
	oversized := `{"resourceMetrics":[],"padding":"` + strings.Repeat("x", metricEnvelopeByteLimit) + `"}`
	for _, payload := range []string{malformed, oversized} {
		if _, err := db.Exec(`INSERT INTO otlp_envelopes (signal,service_name,payload) VALUES ('metrics','other',?)`, payload); err != nil {
			t.Fatal(err)
		}
	}
	selector := metricEnvelope("A", differenceTestPoint("10", "2", "7"))
	if _, err := QueryMetricDifference(db, []byte(selector), 10, 30); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("payload processed before metadata preflight: %v", err)
	}
}

func TestMetricEnvelopePreflightUsesBytesAndOneSnapshot(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	payload := `{"resourceMetrics":[],"text":"é"}`
	if _, err := db.Exec(`INSERT INTO otlp_envelopes (signal,service_name,payload) VALUES ('metrics','other',?)`, payload); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	size, err := metricEnvelopePreflight(tx)
	if err != nil {
		t.Fatal(err)
	}
	if size.count != 1 || size.bytes != len(payload) {
		t.Fatalf("wrong byte size: %+v", size)
	}
	if _, err := db.Exec(`DELETE FROM otlp_envelopes WHERE signal='metrics'`); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := visitMetricEnvelopes(tx, size, func(raw []byte) error { got = string(raw); return nil }); err != nil {
		t.Fatal(err)
	}
	if got != payload {
		t.Fatalf("snapshot changed: %q", got)
	}
}

func TestMetricDifferenceUsesIndependentPointDisproof(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	unrelatedPoint := strings.Replace(differenceTestPoint("20", "2", "8"), `"stringValue":"GET"`, `"stringValue":"POST"`, 1)
	cases := []struct{ name, candidate string }{
		{"incomplete resource", strings.Replace(metricEnvelope("A", unrelatedPoint), `"resource":{"attributes":`, `"resource":{"droppedAttributesCount":1,"attributes":`, 1)},
		{"incomplete scope", strings.Replace(metricEnvelope("A", unrelatedPoint), `"scope":{"name":"sdk","version":"1","attributes":`, `"scope":{"name":"sdk","version":"1","droppedAttributesCount":1,"attributes":`, 1)},
		{"unknown resource field", strings.Replace(metricEnvelope("A", unrelatedPoint), `"resource":{"attributes":`, `"resource":{"futureField":42,"attributes":`, 1)},
		{"unknown scope field", strings.Replace(metricEnvelope("A", unrelatedPoint), `"scope":{"name":"sdk"`, `"scope":{"futureField":42,"name":"sdk"`, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := ingestMetricsForDifference(t, tc.candidate, metricEnvelope("A", first+","+last))
			got, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30)
			if strings.HasPrefix(tc.name, "unknown") {
				if err == nil {
					t.Fatalf("unknown field accepted: %+v", got)
				}
			} else if err != nil || got.Difference != "10" {
				t.Fatalf("independent point mismatch did not disprove identity: %+v %v", got, err)
			}
		})
	}
}

func TestMetricDifferenceRejectsPossibleUnknownIdentity(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	cases := []struct{ name, candidate string }{
		{"unknown resource field", strings.Replace(metricEnvelope("A", first), `"resource":{"attributes":`, `"resource":{"futureField":42,"attributes":`, 1)},
		{"unknown scope field", strings.Replace(metricEnvelope("A", first), `"scope":{"name":"sdk"`, `"scope":{"futureField":42,"name":"sdk"`, 1)},
		{"duplicate point timestamp alias", strings.Replace(metricEnvelope("A", first), `"timeUnixNano":"10"`, `"timeUnixNano":"10","time_unix_nano":"10"`, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := ingestMetricsForDifference(t, tc.candidate, metricEnvelope("A", first+","+last))
			if _, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30); err == nil {
				t.Fatal("possible unknown identity accepted")
			}
		})
	}
}

func TestMetricDifferenceRejectsAmbiguousSibling(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	other := strings.Replace(differenceTestPoint("20", "2", "8"), `"stringValue":"GET"`, `"stringValue":"POST"`, 1)
	other = strings.Replace(other, `"timeUnixNano":"20"`, `"timeUnixNano":"20","time_unix_nano":"20"`, 1)
	data := metricEnvelope("A", first+","+other+","+last)
	db := ingestMetricsForDifference(t, data)
	got, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30)
	if err == nil {
		t.Fatalf("ambiguous sibling accepted: %+v", got)
	}
}

func TestMetricDifferenceHandlesMalformedAttributesByDisproof(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	other := differenceTestPoint("20", "2", "8")
	attrs := `"attributes":[{"key":"method","value":{"stringValue":"GET"}}]`
	extra := `"attributes":[{"key":"package","value":{"stringValue":"other"}},{"value":{"boolValue":true}}]`
	duplicate := `"attributes":[{"key":"method","value":{"stringValue":"POST"}},{"key":"method","value":{"stringValue":"GET"}}]`
	for _, tc := range []struct {
		name, attrs string
		disjoint    bool
	}{{"known extra key", extra, false}, {"duplicate key", duplicate, false}} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := strings.Replace(other, attrs, tc.attrs, 1)
			db := ingestMetricsForDifference(t, metricEnvelope("A", candidate), metricEnvelope("A", first+","+last))
			got, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30)
			if tc.disjoint {
				if err != nil || got.Difference != "10" {
					t.Fatalf("disjoint malformed sibling: %+v %v", got, err)
				}
			} else if err == nil {
				t.Fatal("ambiguous duplicate accepted")
			}
		})
	}
}

func TestMetricDifferenceRejectsDuplicateSchemaAliases(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	duplicate := strings.Replace(metricEnvelope("A", first), `"schemaUrl":"resource-schema"`, `"schemaUrl":"resource-schema","schema_url":"resource-schema"`, 1)
	disjoint := strings.Replace(duplicate, `"stringValue":"GET"`, `"stringValue":"POST"`, 1)
	selector := metricEnvelope("A", first)
	t.Run("point disproof", func(t *testing.T) {
		db := ingestMetricsForDifference(t, disjoint, metricEnvelope("A", first+","+last))
		got, err := QueryMetricDifference(db, []byte(selector), 10, 30)
		if err == nil {
			t.Fatalf("disjoint duplicate alias blocked accepted: %+v", got)
		}
	})
	t.Run("possible match", func(t *testing.T) {
		db := ingestMetricsForDifference(t, duplicate, metricEnvelope("A", first+","+last))
		if _, err := QueryMetricDifference(db, []byte(selector), 10, 30); err == nil {
			t.Fatal("possibly matching duplicate alias accepted")
		}
	})
}

func TestMetricDifferenceRejectsConflictingLaterSameEpoch(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	later := differenceTestPoint("40", "2", "20")
	cases := []struct{ name, point string }{
		{"mixed numeric type", strings.Replace(later, `"asInt":"20"`, `"asDouble":20`, 1)},
		{"missing value flag", strings.Replace(later, `"asInt":`, `"flags":1,"asInt":`, 1)},
		{"decreasing", strings.Replace(later, `"asInt":"20"`, `"asInt":"1"`, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := ingestMetricsForDifference(t, metricEnvelope("A", first+","+last+","+tc.point))
			if _, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30); err == nil {
				t.Fatal("conflicting later point accepted")
			}
		})
	}
}

func TestMetricDifferenceRejectsDuplicateMetricNames(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	selector := metricEnvelope("A", first)
	duplicate := strings.Replace(metricEnvelope("A", first), `"name":"requests"`, `"name":"other","name":"also-other"`, 1)
	possible := strings.Replace(metricEnvelope("A", first), `"name":"requests"`, `"name":"other","name":"requests"`, 1)
	t.Run("disjoint", func(t *testing.T) {
		db := ingestMetricsForDifference(t, duplicate, metricEnvelope("A", first+","+last))
		got, err := QueryMetricDifference(db, []byte(selector), 10, 30)
		if err == nil {
			t.Fatalf("disjoint duplicate names accepted: %+v", got)
		}
	})
	t.Run("possible match", func(t *testing.T) {
		db := ingestMetricsForDifference(t, possible, metricEnvelope("A", first+","+last))
		if _, err := QueryMetricDifference(db, []byte(selector), 10, 30); err == nil {
			t.Fatal("possibly matching duplicate names accepted")
		}
	})
}

func TestMetricDifferenceSelectorLimitsAttributeCount(t *testing.T) {
	attrs := make([]string, metricAttributeLimit+1)
	for i := range attrs {
		attrs[i] = fmt.Sprintf(`{"key":"k%d","value":{"stringValue":"v"}}`, i)
	}
	selector := strings.Replace(metricEnvelope("A", differenceTestPoint("10", "2", "7")), `{"key":"service.name","value":{"stringValue":"app"}},{"key":"region","value":{"intValue":"7"}}`, strings.Join(attrs, ","), 1)
	if err := ValidateMetricSelector([]byte(selector)); err == nil || !strings.Contains(err.Error(), "attribute count limit") {
		t.Fatalf("oversized selector attributes accepted: %v", err)
	}
}

func TestMetricDifferenceUnknownSiblingCannotBeDisproved(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	base := metricEnvelope("A", first+","+last)
	for _, tc := range []struct{ name, needle, replacement string }{
		{"root", `{"resourceMetrics":`, `{"future":{},"resourceMetrics":`},
		{"resource", `{"resource":{"attributes":`, `{"future":{},"resource":{"attributes":`},
		{"scope", `{"scope":{"name":`, `{"future":{},"scope":{"name":`},
		{"metric", `{"name":"requests","unit":`, `{"future":{},"name":"requests","unit":`},
		{"sum", `"sum":{"aggregationTemporality":`, `"sum":{"future":{},"aggregationTemporality":`},
	} {
		for _, child := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/disjoint-child-%v", tc.name, child), func(t *testing.T) {
				data := strings.Replace(base, tc.needle, tc.replacement, 1)
				if data == base {
					t.Fatal("fixture did not inject unknown")
				}
				if child {
					other := strings.Replace(differenceTestPoint("20", "2", "8"), `"stringValue":"GET"`, `"stringValue":"POST"`, 1)
					data = strings.Replace(data, `"dataPoints":[`, `"dataPoints":[`+other+`,`, 1)
				}
				db := ingestMetricsForDifference(t, data)
				if _, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30); err == nil {
					t.Fatal("unknown sibling accepted")
				}
			})
		}
	}
}

func TestMetricDifferenceCountsUnrelatedPointsBeforeDecode(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	oversized := `{"resourceMetrics":[{"scopeMetrics":[{"metrics":[{"name":"unrelated","sum":{"dataPoints":[` + strings.Repeat(`{},`, 100000) + `{}]}}]}]}]}`
	for _, raw := range []string{metricEnvelope("A", first+","+last), oversized} {
		if _, err = db.Exec(`INSERT INTO otlp_envelopes (signal, service_name, payload) VALUES ('metrics', 'other', ?)`, raw); err != nil {
			t.Fatal(err)
		}
	}
	_, err = QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30)
	if err == nil || !strings.Contains(err.Error(), "point scan limit") {
		t.Fatalf("unrelated points did not hit limit: %v", err)
	}
}

func TestMetricDifferenceAcceptsNullUnusedOneof(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	nullGauge := func(s string) string {
		return strings.Replace(s, `"name":"requests"`, `"name":"requests","gauge":null`, 1)
	}
	db := ingestMetricsForDifference(t, nullGauge(metricEnvelope("A", first+","+last)))
	got, err := QueryMetricDifference(db, []byte(nullGauge(metricEnvelope("A", first))), 10, 30)
	if err != nil || got.Difference != "10" {
		t.Fatalf("null oneof: %+v %v", got, err)
	}
}

func TestMetricDifferenceRejectsEarlierSameEpochDecrease(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	db := ingestMetricsForDifference(t, metricEnvelope("A", differenceTestPoint("9", "2", "99")+","+first+","+last))
	if _, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30); err == nil {
		t.Fatal("earlier decrease accepted")
	}
}

func TestMetricDifferenceRejectsMalformedAttributesAnywhere(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	last := differenceTestPoint("30", "2", "17")
	selected := metricEnvelope("A", first+","+last)
	unrelated := strings.Replace(metricEnvelope("A", first), `"name":"requests"`, `"name":"other"`, 1)
	cases := []struct{ name, candidate string }{
		{"resource", strings.Replace(unrelated, `"resource":{"attributes":[`, `"resource":{"attributes":[{"value":{"boolValue":true}},`, 1)},
		{"scope", strings.Replace(unrelated, `"scope":{"name":"sdk","version":"1","attributes":[`, `"scope":{"name":"sdk","version":"1","attributes":[{"key":"broken"},`, 1)},
		{"point", strings.Replace(unrelated, `"key":"method","value":{"stringValue":"GET"}`, `"value":{"boolValue":true}`, 1)},
		{"nested map", strings.Replace(unrelated, `"stringValue":"GET"`, `"kvlistValue":{"values":[{"value":{"boolValue":true}}]}`, 1)},
		{"exemplar", strings.Replace(unrelated, `"asInt":"7"`, `"asInt":"7","exemplars":[{"filteredAttributes":[{"value":{"boolValue":true}}]}]`, 1)},
		{"missing metric type", strings.Replace(unrelated, `"sum":{"aggregationTemporality":2,"isMonotonic":true,"dataPoints":[`+first+`]}`, `"gauge":null`, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.candidate == unrelated {
				t.Fatal("fixture did not change")
			}
			db := ingestMetricsForDifference(t, tc.candidate, selected)
			if _, err := QueryMetricDifference(db, []byte(metricEnvelope("A", first)), 10, 30); err == nil {
				t.Fatal("malformed unrelated envelope accepted")
			}
		})
	}
}

func TestMetricSelectorPreflightsPointsAndNesting(t *testing.T) {
	first := differenceTestPoint("10", "2", "7")
	malformed := strings.Replace(differenceTestPoint("20", "2", "8"), `"asInt":"8"`, `"future":8`, 1)
	for _, tc := range []struct{ name, selector, reason string }{
		{"extra point before decode", metricEnvelope("A", first+","+malformed), "point scan limit"},
		{"nesting before decode", `{"x":` + strings.Repeat(`[`, metricJSONRecursionLimit+2) + `0` + strings.Repeat(`]`, metricJSONRecursionLimit+2) + `}`, "nesting limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateMetricSelector([]byte(tc.selector)); err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("preflight: %v", err)
			}
		})
	}
}

func TestMetricEnvelopePreflightRejectsAggregateBytes(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO otlp_envelopes (signal,service_name,payload) SELECT 'metrics','other', ('{"padding":"' || repeat('x', 3145728) || '"}')::JSON FROM range(22)`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := metricEnvelopePreflight(tx); err == nil || !strings.Contains(err.Error(), "scan byte limit") {
		t.Fatalf("aggregate preflight: %v", err)
	}
}
