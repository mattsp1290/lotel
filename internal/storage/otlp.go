package storage

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"
)

// otlpAttr represents an OTLP key-value attribute.
type otlpAttr struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

// otlpValue represents an OTLP typed value.
type otlpValue struct {
	BytesValue  *string     `json:"bytesValue,omitempty"`
	ArrayValue  *otlpArray  `json:"arrayValue,omitempty"`
	KvlistValue *otlpKvlist `json:"kvlistValue,omitempty"`
	StringValue *string     `json:"stringValue,omitempty"`
	IntValue    *otlpInt    `json:"intValue,omitempty"`
	BoolValue   *bool       `json:"boolValue,omitempty"`
	DoubleValue *otlpDouble `json:"doubleValue,omitempty"`
}

type otlpArray struct {
	Values []otlpValue `json:"values"`
}
type otlpKvlist struct {
	Values []otlpAttr `json:"values"`
}

type otlpDouble float64

func (d *otlpDouble) UnmarshalJSON(b []byte) error {
	var value float64
	if len(b) > 0 && b[0] == '"' {
		var text string
		if err := json.Unmarshal(b, &text); err != nil {
			return err
		}
		switch text {
		case "NaN":
			value = math.NaN()
		case "Infinity":
			value = math.Inf(1)
		case "-Infinity":
			value = math.Inf(-1)
		default:
			return fmt.Errorf("invalid OTLP double %q", text)
		}
	} else if err := json.Unmarshal(b, &value); err != nil {
		return err
	}
	*d = otlpDouble(value)
	return nil
}

func (d otlpDouble) Any() any {
	value := float64(d)
	if math.IsNaN(value) {
		return "NaN"
	}
	if math.IsInf(value, 1) {
		return "Infinity"
	}
	if math.IsInf(value, -1) {
		return "-Infinity"
	}
	return value
}

func (d otlpDouble) MarshalJSON() ([]byte, error) { return json.Marshal(d.Any()) }

func (v otlpValue) Any() any {
	switch {
	case v.StringValue != nil:
		return *v.StringValue
	case v.IntValue != nil:
		n, err := strconv.ParseInt(string(*v.IntValue), 10, 64)
		if err != nil {
			return string(*v.IntValue)
		}
		return n
	case v.BoolValue != nil:
		return *v.BoolValue
	case v.DoubleValue != nil:
		return v.DoubleValue.Any()
	case v.BytesValue != nil:
		return *v.BytesValue
	case v.ArrayValue != nil:
		out := make([]any, len(v.ArrayValue.Values))
		for i, item := range v.ArrayValue.Values {
			out[i] = item.Any()
		}
		return out
	case v.KvlistValue != nil:
		return attrsToMap(v.KvlistValue.Values)
	default:
		return nil
	}
}

func (v otlpValue) String() string {
	value := v.Any()
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	data, _ := json.Marshal(value)
	return string(data)
}

func attrsToMap(attrs []otlpAttr) map[string]any {
	out := make(map[string]any, len(attrs))
	for _, attr := range attrs {
		out[attr.Key] = attr.Value.Any()
	}
	return out
}

type otlpResource struct {
	Attributes []otlpAttr `json:"attributes"`
}
type otlpScope struct {
	Name       string     `json:"name"`
	Version    string     `json:"version"`
	Attributes []otlpAttr `json:"attributes"`
}
type otlpSpanEvent struct {
	TimeUnixNano otlpNano   `json:"timeUnixNano"`
	Name         string     `json:"name"`
	Attributes   []otlpAttr `json:"attributes"`
}
type otlpSpanLink struct {
	TraceID    string     `json:"traceId"`
	SpanID     string     `json:"spanId"`
	TraceState string     `json:"traceState"`
	Attributes []otlpAttr `json:"attributes"`
	Flags      int        `json:"flags"`
}

// otlpNano handles nanosecond timestamps that may be strings or integers.
type otlpNano int64

func (n *otlpNano) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		*n = otlpNano(v)
		return nil
	}
	var v int64
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*n = otlpNano(v)
	return nil
}

func (n otlpNano) Time() time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, int64(n)).UTC()
}

type otlpMetric struct {
	Name                 string            `json:"name"`
	Description          string            `json:"description"`
	Unit                 string            `json:"unit"`
	Sum                  *otlpSum          `json:"sum,omitempty"`
	Gauge                *otlpGauge        `json:"gauge,omitempty"`
	ExponentialHistogram *otlpExpHistogram `json:"exponentialHistogram,omitempty"`
	Summary              *otlpSummary      `json:"summary,omitempty"`
	Histogram            *otlpHistogram    `json:"histogram,omitempty"`
}

type otlpSum struct {
	DataPoints             []otlpDataPoint `json:"dataPoints"`
	AggregationTemporality int             `json:"aggregationTemporality"`
	IsMonotonic            bool            `json:"isMonotonic"`
}

type otlpGauge struct {
	DataPoints []otlpDataPoint `json:"dataPoints"`
}

type otlpHistogram struct {
	DataPoints             []otlpHistogramDP `json:"dataPoints"`
	AggregationTemporality int               `json:"aggregationTemporality"`
}

type otlpDataPoint struct {
	StartTimeUnixNano otlpNano    `json:"startTimeUnixNano"`
	Attributes        []otlpAttr  `json:"attributes"`
	TimeUnixNano      otlpNano    `json:"timeUnixNano"`
	AsInt             *otlpInt    `json:"asInt,omitempty"`
	AsDouble          *otlpDouble `json:"asDouble,omitempty"`
}

type otlpHistogramDP struct {
	StartTimeUnixNano otlpNano     `json:"startTimeUnixNano"`
	Min               *otlpDouble  `json:"min,omitempty"`
	Max               *otlpDouble  `json:"max,omitempty"`
	ExplicitBounds    []otlpDouble `json:"explicitBounds"`
	BucketCounts      []otlpInt    `json:"bucketCounts"`

	Attributes   []otlpAttr  `json:"attributes"`
	TimeUnixNano otlpNano    `json:"timeUnixNano"`
	Count        *otlpInt    `json:"count,omitempty"`
	Sum          *otlpDouble `json:"sum,omitempty"`
}

type metricPoint struct {
	metricType                                                  string
	value                                                       any
	timestamp                                                   time.Time
	startTimestamp                                              time.Time
	temporality                                                 int
	monotonic                                                   bool
	attributes                                                  []otlpAttr
	count                                                       any
	sum, min, max                                               any
	explicitBounds, bucketCounts                                any
	expScale, expZeroCount, expPositive, expNegative, quantiles any
}

func finiteDouble(d *otlpDouble) any {
	if d == nil {
		return nil
	}
	n := float64(*d)
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return nil
	}
	return n
}
func intField(text *otlpInt) any {
	if text == nil {
		return nil
	}
	n, err := strconv.ParseInt(string(*text), 10, 64)
	if err != nil {
		return nil
	}
	return n
}
func bucketJSON(values []otlpInt) string {
	out := make([]any, len(values))
	for i := range values {
		out[i] = intField(&values[i])
	}
	return jsonText(out)
}
func expBucketsJSON(b otlpExpBuckets) string {
	return jsonText(map[string]any{"offset": b.Offset, "bucket_counts": json.RawMessage(bucketJSON(b.BucketCounts))})
}
func extractDataPoints(m otlpMetric) []metricPoint {
	var points []metricPoint
	addNumber := func(kind string, dp otlpDataPoint, temporality int, monotonic bool) {
		var value any
		if dp.AsDouble != nil {
			value = finiteDouble(dp.AsDouble)
		} else if dp.AsInt != nil {
			n, err := strconv.ParseFloat(string(*dp.AsInt), 64)
			if err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) {
				value = n
			}
		}
		points = append(points, metricPoint{metricType: kind, value: value, timestamp: dp.TimeUnixNano.Time(), startTimestamp: dp.StartTimeUnixNano.Time(), temporality: temporality, monotonic: monotonic, attributes: dp.Attributes})
	}
	if m.Sum != nil {
		for _, dp := range m.Sum.DataPoints {
			addNumber("sum", dp, m.Sum.AggregationTemporality, m.Sum.IsMonotonic)
		}
	}
	if m.Gauge != nil {
		for _, dp := range m.Gauge.DataPoints {
			addNumber("gauge", dp, 0, false)
		}
	}
	if m.Histogram != nil {
		for _, dp := range m.Histogram.DataPoints {
			points = append(points, metricPoint{metricType: "histogram", value: finiteDouble(dp.Sum), timestamp: dp.TimeUnixNano.Time(), startTimestamp: dp.StartTimeUnixNano.Time(), temporality: m.Histogram.AggregationTemporality, attributes: dp.Attributes, count: intField(dp.Count), sum: finiteDouble(dp.Sum), min: finiteDouble(dp.Min), max: finiteDouble(dp.Max), explicitBounds: jsonText(dp.ExplicitBounds), bucketCounts: bucketJSON(dp.BucketCounts)})
		}
	}
	if m.ExponentialHistogram != nil {
		for _, dp := range m.ExponentialHistogram.DataPoints {
			points = append(points, metricPoint{metricType: "exponential_histogram", value: finiteDouble(dp.Sum), timestamp: dp.TimeUnixNano.Time(), startTimestamp: dp.StartTimeUnixNano.Time(), temporality: m.ExponentialHistogram.AggregationTemporality, attributes: dp.Attributes, count: intField(dp.Count), sum: finiteDouble(dp.Sum), min: finiteDouble(dp.Min), max: finiteDouble(dp.Max), expScale: dp.Scale, expZeroCount: intField(dp.ZeroCount), expPositive: expBucketsJSON(dp.Positive), expNegative: expBucketsJSON(dp.Negative)})
		}
	}
	if m.Summary != nil {
		for _, dp := range m.Summary.DataPoints {
			points = append(points, metricPoint{metricType: "summary", value: finiteDouble(dp.Sum), timestamp: dp.TimeUnixNano.Time(), startTimestamp: dp.StartTimeUnixNano.Time(), attributes: dp.Attributes, count: intField(dp.Count), sum: finiteDouble(dp.Sum), quantiles: jsonText(dp.QuantileValues)})
		}
	}
	return points
}

type otlpExpHistogram struct {
	DataPoints             []otlpExpHistogramDP `json:"dataPoints"`
	AggregationTemporality int                  `json:"aggregationTemporality"`
}

type otlpExpBuckets struct {
	Offset       int       `json:"offset"`
	BucketCounts []otlpInt `json:"bucketCounts"`
}

type otlpExpHistogramDP struct {
	Attributes        []otlpAttr     `json:"attributes"`
	StartTimeUnixNano otlpNano       `json:"startTimeUnixNano"`
	TimeUnixNano      otlpNano       `json:"timeUnixNano"`
	Count             *otlpInt       `json:"count"`
	Sum               *otlpDouble    `json:"sum"`
	Min               *otlpDouble    `json:"min"`
	Max               *otlpDouble    `json:"max"`
	Scale             int            `json:"scale"`
	ZeroCount         *otlpInt       `json:"zeroCount"`
	ZeroThreshold     otlpDouble     `json:"zeroThreshold"`
	Positive          otlpExpBuckets `json:"positive"`
	Negative          otlpExpBuckets `json:"negative"`
}

type otlpSummary struct {
	DataPoints []otlpSummaryDP `json:"dataPoints"`
}

type otlpQuantile struct {
	Quantile otlpDouble `json:"quantile"`
	Value    otlpDouble `json:"value"`
}

type otlpSummaryDP struct {
	Attributes        []otlpAttr     `json:"attributes"`
	StartTimeUnixNano otlpNano       `json:"startTimeUnixNano"`
	TimeUnixNano      otlpNano       `json:"timeUnixNano"`
	Count             *otlpInt       `json:"count"`
	Sum               *otlpDouble    `json:"sum"`
	QuantileValues    []otlpQuantile `json:"quantileValues"`
}

// otlpInt accepts both JSON encodings permitted for protobuf integer fields.
type otlpInt string

func (n *otlpInt) UnmarshalJSON(b []byte) error {
	var text string
	if len(b) > 0 && b[0] == '"' {
		if err := json.Unmarshal(b, &text); err != nil {
			return err
		}
	} else {
		var number json.Number
		if err := json.Unmarshal(b, &number); err != nil {
			return err
		}
		text = number.String()
	}
	*n = otlpInt(text)
	return nil
}
