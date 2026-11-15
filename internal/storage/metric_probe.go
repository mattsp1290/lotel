package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

type metricObject struct {
	fields    map[string][]json.RawMessage
	duplicate bool
}

func parseMetricObject(raw json.RawMessage) (metricObject, error) {
	result := metricObject{fields: make(map[string][]json.RawMessage)}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return result, fmt.Errorf("expected object")
	}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return result, err
		}
		key, ok := token.(string)
		if !ok {
			return result, fmt.Errorf("invalid key")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return result, err
		}
		result.fields[key] = append(result.fields[key], value)
		if len(result.fields[key]) > 1 {
			result.duplicate = true
		}
	}
	if _, err := decoder.Token(); err != nil {
		return result, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return result, fmt.Errorf("trailing object data")
	}
	return result, nil
}

func (o metricObject) values(names ...string) []json.RawMessage {
	var values []json.RawMessage
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		values = append(values, o.fields[name]...)
	}
	return values
}

func metricHasUnknown(o metricObject, allowed ...string) bool {
	known := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		known[key] = true
	}
	for key := range o.fields {
		if !known[key] {
			return true
		}
	}
	return false
}

func metricStringDisjoint(o metricObject, expected string, names ...string) bool {
	values := o.values(names...)
	if len(values) == 0 {
		return expected != ""
	}
	for _, raw := range values {
		var value string
		if json.Unmarshal(raw, &value) != nil || value == expected {
			return false
		}
	}
	return true
}

func metricRawArrays(o metricObject, names ...string) ([][]json.RawMessage, bool, error) {
	values := o.values(names...)
	var arrays [][]json.RawMessage
	for _, raw := range values {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, false, err
		}
		arrays = append(arrays, items)
	}
	return arrays, len(values) > 1, nil
}

func metricDroppedKnownZero(o metricObject) bool {
	values := o.values("droppedAttributesCount", "dropped_attributes_count")
	if len(values) == 0 {
		return true
	}
	if len(values) != 1 {
		return false
	}
	var text string
	if len(values[0]) > 0 && values[0][0] == '"' {
		if json.Unmarshal(values[0], &text) != nil {
			return false
		}
	} else {
		text = string(values[0])
	}
	n, err := strconv.ParseUint(text, 10, 32)
	return err == nil && n == 0
}

func metricAttrsDisjoint(container json.RawMessage, selected map[string][]byte, canDrop bool) bool {
	if len(container) == 0 || bytes.Equal(bytes.TrimSpace(container), []byte("null")) {
		return len(selected) > 0
	}
	obj, err := parseMetricObject(container)
	if err != nil {
		return false
	}
	if canDrop {
		canDrop = !metricDroppedKnownZero(obj)
	}
	arrays, ambiguous, err := metricRawArrays(obj, "attributes", "attributes")
	if err != nil {
		return false
	}
	if len(arrays) == 0 {
		return !canDrop && len(selected) > 0
	}
	for _, array := range arrays {
		if !metricAttrArrayDisjoint(array, selected, canDrop || ambiguous) {
			return false
		}
	}
	return true
}

func metricAttrArrayDisjoint(items []json.RawMessage, selected map[string][]byte, incomplete bool) bool {
	if len(items) > metricAttributeLimit {
		return false
	}
	counts := make(map[string]int, len(items))
	decoded := make([]*commonpb.KeyValue, len(items))
	for _, raw := range items {
		object, err := parseMetricObject(raw)
		if err != nil {
			return false
		}
		keys := object.values("key")
		if len(keys) != 1 {
			return false
		}
		var key string
		if json.Unmarshal(keys[0], &key) != nil || key == "" || len(object.values("keyStrindex", "key_strindex")) != 0 {
			return false
		}
		counts[key]++
	}
	for i, raw := range items {
		kv := &commonpb.KeyValue{}
		if err := (protojson.UnmarshalOptions{DiscardUnknown: false, RecursionLimit: metricJSONRecursionLimit}).Unmarshal(raw, kv); err != nil {
			incomplete = true
			continue
		}
		if err := canonicalAttrs([]*commonpb.KeyValue{kv}, false); err != nil {
			incomplete = true
			continue
		}
		decoded[i] = kv
	}
	valid := 0
	for _, kv := range decoded {
		if kv == nil {
			continue
		}
		if counts[kv.Key] != 1 {
			incomplete = true
			continue
		}
		value, err := encodedIdentity(kv)
		if err != nil {
			incomplete = true
			continue
		}
		valid++
		expected, ok := selected[kv.Key]
		if !ok || !bytes.Equal(value, expected) {
			return true
		}
	}
	return !incomplete && valid != len(selected)
}

func metricObjects(obj metricObject, names ...string) ([]json.RawMessage, bool, error) {
	arrays, aliases, err := metricRawArrays(obj, names...)
	if err != nil {
		return nil, false, err
	}
	var result []json.RawMessage
	for _, array := range arrays {
		result = append(result, array...)
	}
	return result, obj.duplicate || aliases, nil
}

func metricContainerAttrsDisjoint(o metricObject, field string, selected map[string][]byte, canDrop bool) bool {
	values := o.values(field)
	if len(values) == 0 {
		return metricAttrsDisjoint(nil, selected, canDrop)
	}
	for _, value := range values {
		if !metricAttrsDisjoint(value, selected, canDrop) {
			return false
		}
	}
	return true
}

func metricScopeDisjoint(o metricObject, id metricSelection) bool {
	values := o.values("scope")
	if len(values) == 0 {
		return id.scopeName != "" || id.scopeVersion != "" || len(id.scopeAttrs) > 0
	}
	for _, raw := range values {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			if id.scopeName == "" && id.scopeVersion == "" && len(id.scopeAttrs) == 0 {
				return false
			}
			continue
		}
		scope, err := parseMetricObject(raw)
		if err != nil {
			return false
		}
		if !metricStringDisjoint(scope, id.scopeName, "name") && !metricStringDisjoint(scope, id.scopeVersion, "version") && !metricAttrsDisjoint(raw, id.scopeAttrs, true) {
			return false
		}
	}
	return true
}

func pruneMetricEnvelope(raw []byte, id metricSelection, budget *int) ([]byte, bool, error) {
	charge := func(n int) error {
		*budget += n
		if *budget > metricCandidateByteLimit {
			return fmt.Errorf("indeterminate: metric candidate processing limit exceeded")
		}
		return nil
	}
	if err := charge(len(raw)); err != nil {
		return nil, false, err
	}
	root, err := parseMetricObject(raw)
	if err != nil {
		return nil, false, fmt.Errorf("indeterminate: invalid metrics envelope: %w", err)
	}
	resources, rootAmbiguous, err := metricObjects(root, "resourceMetrics", "resource_metrics")
	if err != nil {
		return nil, false, fmt.Errorf("indeterminate: invalid resource list: %w", err)
	}
	if len(resources) == 0 && metricHasUnknown(root, "resourceMetrics", "resource_metrics") {
		return nil, false, fmt.Errorf("indeterminate: unsupported envelope fields")
	}
	var selectedResources []json.RawMessage
	for _, resourceRaw := range resources {
		if err := charge(len(resourceRaw)); err != nil {
			return nil, false, err
		}
		resource, err := parseMetricObject(resourceRaw)
		if err != nil {
			return nil, false, fmt.Errorf("indeterminate: invalid resource envelope: %w", err)
		}
		if metricStringDisjoint(resource, id.resourceSchema, "schemaUrl", "schema_url") || metricContainerAttrsDisjoint(resource, "resource", id.resourceAttrs, true) {
			continue
		}
		scopes, resAmbiguous, err := metricObjects(resource, "scopeMetrics", "scope_metrics")
		if err != nil {
			return nil, false, fmt.Errorf("indeterminate: invalid scope list: %w", err)
		}
		if len(scopes) == 0 && metricHasUnknown(resource, "resource", "schemaUrl", "schema_url", "scopeMetrics", "scope_metrics") {
			return nil, false, fmt.Errorf("indeterminate: unsupported resource envelope fields")
		}
		var selectedScopes []json.RawMessage
		for _, scopeRaw := range scopes {
			if err := charge(len(scopeRaw)); err != nil {
				return nil, false, err
			}
			scope, err := parseMetricObject(scopeRaw)
			if err != nil {
				return nil, false, fmt.Errorf("indeterminate: invalid scope envelope: %w", err)
			}
			if metricStringDisjoint(scope, id.scopeSchema, "schemaUrl", "schema_url") || metricScopeDisjoint(scope, id) {
				continue
			}
			metrics, scopeAmbiguous, err := metricObjects(scope, "metrics")
			if err != nil {
				return nil, false, fmt.Errorf("indeterminate: invalid metric list: %w", err)
			}
			if len(metrics) == 0 && metricHasUnknown(scope, "scope", "schemaUrl", "schema_url", "metrics") {
				return nil, false, fmt.Errorf("indeterminate: unsupported scope envelope fields")
			}
			var selectedMetrics []json.RawMessage
			for _, metricRaw := range metrics {
				if err := charge(len(metricRaw)); err != nil {
					return nil, false, err
				}
				metric, err := parseMetricObject(metricRaw)
				if err != nil {
					return nil, false, fmt.Errorf("indeterminate: invalid metric: %w", err)
				}
				if metricStringDisjoint(metric, id.name, "name") || metricStringDisjoint(metric, id.unit, "unit") {
					continue
				}
				pruned, keep, err := pruneMetricPoints(metric, id, budget)
				if err != nil {
					return nil, false, err
				}
				if !keep {
					continue
				}
				if metric.duplicate || scopeAmbiguous {
					return nil, false, fmt.Errorf("indeterminate: duplicate metric or scope fields")
				}
				selectedMetrics = append(selectedMetrics, pruned)
			}
			if len(selectedMetrics) == 0 {
				continue
			}
			if scope.duplicate || resAmbiguous {
				return nil, false, fmt.Errorf("indeterminate: duplicate scope or resource fields")
			}
			scopeMap := metricSingleFields(scope)
			delete(scopeMap, "metrics")
			scopeMap["metrics"], err = json.Marshal(selectedMetrics)
			if err != nil {
				return nil, false, err
			}
			encoded, err := json.Marshal(scopeMap)
			if err != nil {
				return nil, false, err
			}
			selectedScopes = append(selectedScopes, encoded)
		}
		if len(selectedScopes) == 0 {
			continue
		}
		if resource.duplicate || rootAmbiguous {
			return nil, false, fmt.Errorf("indeterminate: duplicate resource or root fields")
		}
		resourceMap := metricSingleFields(resource)
		delete(resourceMap, "scope_metrics")
		resourceMap["scopeMetrics"], err = json.Marshal(selectedScopes)
		if err != nil {
			return nil, false, err
		}
		encoded, err := json.Marshal(resourceMap)
		if err != nil {
			return nil, false, err
		}
		selectedResources = append(selectedResources, encoded)
	}
	if len(selectedResources) == 0 {
		return nil, false, nil
	}
	if root.duplicate {
		return nil, false, fmt.Errorf("indeterminate: duplicate envelope fields")
	}
	rootMap := metricSingleFields(root)
	delete(rootMap, "resource_metrics")
	rootMap["resourceMetrics"], err = json.Marshal(selectedResources)
	if err != nil {
		return nil, false, err
	}
	cleaned, err := json.Marshal(rootMap)
	return cleaned, true, err
}

func metricSingleFields(o metricObject) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage, len(o.fields))
	for key, values := range o.fields {
		if len(values) > 0 {
			result[key] = values[0]
		}
	}
	return result
}

func pruneMetricPoints(metric metricObject, id metricSelection, budget *int) (json.RawMessage, bool, error) {
	kinds := []struct{ camel, snake string }{{"sum", "sum"}, {"gauge", "gauge"}, {"histogram", "histogram"}, {"exponentialHistogram", "exponential_histogram"}, {"summary", "summary"}}
	result := metricSingleFields(metric)
	recognized := 0
	seenPoints := 0
	keepAny := false
	for _, kind := range kinds {
		values := metric.values(kind.camel)
		if kind.snake != kind.camel {
			values = append(values, metric.values(kind.snake)...)
		}
		if len(values) == 0 {
			continue
		}
		recognized += len(values)
		var retained []json.RawMessage
		for _, raw := range values {
			*budget += len(raw)
			if *budget > metricCandidateByteLimit {
				return nil, false, fmt.Errorf("indeterminate: metric candidate processing limit exceeded")
			}
			container, err := parseMetricObject(raw)
			if err != nil {
				return nil, false, fmt.Errorf("indeterminate: invalid metric data: %w", err)
			}
			points, ambiguous, err := metricObjects(container, "dataPoints", "data_points")
			if err != nil {
				return nil, false, fmt.Errorf("indeterminate: invalid data points: %w", err)
			}
			seenPoints += len(points)
			if len(points) == 0 && metricHasUnknown(container, "dataPoints", "data_points", "aggregationTemporality", "aggregation_temporality", "isMonotonic", "is_monotonic") {
				return nil, false, fmt.Errorf("indeterminate: unsupported metric data fields")
			}
			var selectedPoints []json.RawMessage
			for _, pointRaw := range points {
				*budget += len(pointRaw)
				if *budget > metricCandidateByteLimit {
					return nil, false, fmt.Errorf("indeterminate: metric candidate processing limit exceeded")
				}
				if metricAttrsDisjoint(pointRaw, id.pointAttrs, false) {
					continue
				}
				selectedPoints = append(selectedPoints, pointRaw)
			}
			if len(selectedPoints) == 0 {
				continue
			}
			if container.duplicate || ambiguous || len(values) > 1 {
				return nil, false, fmt.Errorf("indeterminate: duplicate metric data fields")
			}
			fields := metricSingleFields(container)
			delete(fields, "data_points")
			fields["dataPoints"], err = json.Marshal(selectedPoints)
			if err != nil {
				return nil, false, err
			}
			encoded, err := json.Marshal(fields)
			if err != nil {
				return nil, false, err
			}
			retained = append(retained, encoded)
		}
		delete(result, kind.camel)
		delete(result, kind.snake)
		if len(retained) > 0 {
			if len(retained) > 1 {
				return nil, false, fmt.Errorf("indeterminate: duplicate metric data")
			}
			result[kind.camel] = retained[0]
			keepAny = true
		}
	}
	if recognized == 0 {
		return nil, false, fmt.Errorf("indeterminate: unsupported metric data")
	}
	if seenPoints == 0 && metricHasUnknown(metric, "name", "description", "unit", "metadata", "sum", "gauge", "histogram", "exponentialHistogram", "exponential_histogram", "summary") {
		return nil, false, fmt.Errorf("indeterminate: unsupported metric fields")
	}
	if !keepAny {
		return nil, false, nil
	}
	if recognized > 1 {
		return nil, false, fmt.Errorf("indeterminate: multiple metric data types")
	}
	encoded, err := json.Marshal(result)
	return encoded, true, err
}
