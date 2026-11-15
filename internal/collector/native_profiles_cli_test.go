package collector_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
)

func TestNativeProfilesAndCompleteOTLP(t *testing.T) {
	f := newNativeCLIFixture(t, "")
	configPath := filepath.Join(f.dir, "lotel-collector.yaml")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(config), "  pipelines:\n", "  pipelines:\n    profiles:\n      receivers: [otlp]\n      processors: []\n      exporters: [file/profiles]\n", 1)
	text = strings.Replace(text, "exporters:\n", "exporters:\n  file/profiles:\n    path: /data/profiles/profiles.jsonl\n    append: true\n", 1)
	text = strings.Replace(text, "interval: 1h", "interval: 50ms", 1)
	if err := os.WriteFile(configPath, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := f.run("start", "--backend", "native", "--wait"); err != nil {
		t.Fatalf("start native profiles: %v\n%s", err, out)
	}
	now := time.Now().UnixNano()
	payload := fmt.Sprintf(`{"resourceProfiles":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"native-profile-test"}}]},"scopeProfiles":[{"scope":{"name":"native-profile-test"},"profiles":[{"profileId":"22222222222222222222222222222222","timeUnixNano":"%d","durationNano":"1234567","sampleType":{"typeStrindex":1,"unitStrindex":2},"periodType":{"typeStrindex":1,"unitStrindex":2},"period":"1000","samples":[{"stackIndex":0,"values":["9007199254740993"],"timestampsUnixNano":["%d"]}],"originalPayloadFormat":"fixture","originalPayload":"AAEC/w=="}]}]}],"dictionary":{"stringTable":["","cpu","nanoseconds"],"stackTable":[{"locationIndices":[]}]}}`, now, now+1)
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Post(f.endpoint+"/v1development/profiles", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	reply, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("send profiles: %v, %s, %s", err, response.Status, reply)
	}
	query := func(command ...string) ([]storage.OTLPResult, error) {
		out, err := f.run(append(command, "--service", "native-profile-test", "--since", "1m")...)
		var rows []storage.OTLPResult
		if err == nil {
			err = json.Unmarshal(out, &rows)
		}
		return rows, err
	}
	var rows []storage.OTLPResult
	deadline := time.Now().Add(15 * time.Second)
	for {
		rows, err = query("query", "profiles")
		if err == nil && len(rows) == 1 {
			break
		}
		if time.Now().After(deadline) {
			log, _ := os.ReadFile(filepath.Join(f.home, ".lotel", "collector.log"))
			t.Fatalf("profiles not indexed automatically: %v, rows %d, reply %s\n%s", err, len(rows), reply, log)
		}
		time.Sleep(50 * time.Millisecond)
	}
	exported, err := os.ReadFile(filepath.Join(f.home, ".lotel", "data", "profiles", "profiles.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	decode := func(data []byte) any {
		t.Helper()
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	if !reflect.DeepEqual(decode(exported), decode(rows[0].Payload)) || !bytes.Contains(rows[0].Payload, []byte(`"9007199254740993"`)) {
		t.Fatalf("profile envelope or exact sample value changed: %s", rows[0].Payload)
	}
	if out, err := f.run("stop"); err != nil {
		t.Fatalf("stop: %v\n%s", err, out)
	}
	for range 2 {
		if out, err := f.run("ingest", "--full"); err != nil {
			t.Fatalf("full profiles rebuild: %v\n%s", err, out)
		}
		rebuilt, err := query("query", "otlp", "--signal", "profiles")
		if err != nil || len(rebuilt) != 1 || !reflect.DeepEqual(decode(rows[0].Payload), decode(rebuilt[0].Payload)) {
			t.Fatalf("full rebuild changed or duplicated profiles: %v, %+v", err, rebuilt)
		}
	}
}
