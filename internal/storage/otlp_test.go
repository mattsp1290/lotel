package storage

import (
	"encoding/json"
	"testing"
)

func TestAnyValue(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`{"stringValue":"hello"}`, `"hello"`},
		{`{"boolValue":true}`, `true`},
		{`{"intValue":"9007199254740993"}`, `9007199254740993`},
		{`{"intValue":"invalid"}`, `"invalid"`},
		{`{"doubleValue":1.5}`, `1.5`},
		{`{"doubleValue":"NaN"}`, `"NaN"`},
		{`{"doubleValue":"Infinity"}`, `"Infinity"`},
		{`{"doubleValue":"-Infinity"}`, `"-Infinity"`},
		{`{"bytesValue":"aGVsbG8="}`, `"aGVsbG8="`},
		{`{"arrayValue":{"values":[{"kvlistValue":{"values":[{"key":"role","value":{"stringValue":"user"}}]}}]}}`, `[{"role":"user"}]`},
		{`{}`, `null`},
	} {
		t.Run(tc.input, func(t *testing.T) {
			var v otlpValue
			if err := json.Unmarshal([]byte(tc.input), &v); err != nil {
				t.Fatal(err)
			}
			got, err := json.Marshal(v.Any())
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}
