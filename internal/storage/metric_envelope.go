package storage

import (
	"encoding/json"
	"fmt"
)

func rawArray(fields map[string]json.RawMessage, camel, snake string) ([]json.RawMessage, error) {
	raw, ok := fields[camel]
	if other, exists := fields[snake]; exists && snake != camel {
		if ok {
			return nil, fmt.Errorf("duplicate field aliases")
		}
		raw = other
		ok = true
	}
	if !ok {
		return nil, nil
	}
	var result []json.RawMessage
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func selectRawMetric(raw []byte, ri, si, mi int, metadataOnly bool) ([]byte, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	resources, err := rawArray(root, "resourceMetrics", "resource_metrics")
	if err != nil || ri >= len(resources) {
		return nil, fmt.Errorf("invalid resource envelope")
	}
	var resource map[string]json.RawMessage
	if err := json.Unmarshal(resources[ri], &resource); err != nil {
		return nil, err
	}
	scopes, err := rawArray(resource, "scopeMetrics", "scope_metrics")
	if err != nil || si >= len(scopes) {
		return nil, fmt.Errorf("invalid scope envelope")
	}
	var scope map[string]json.RawMessage
	if err := json.Unmarshal(scopes[si], &scope); err != nil {
		return nil, err
	}
	metrics, err := rawArray(scope, "metrics", "metrics")
	if err != nil || mi >= len(metrics) {
		return nil, fmt.Errorf("invalid metric envelope")
	}
	selectedMetrics := []json.RawMessage{}
	if !metadataOnly {
		selectedMetrics = append(selectedMetrics, metrics[mi])
	}
	scope["metrics"], err = json.Marshal(selectedMetrics)
	if err != nil {
		return nil, err
	}
	scopeRaw, err := json.Marshal(scope)
	if err != nil {
		return nil, err
	}
	delete(resource, "scope_metrics")
	resource["scopeMetrics"], err = json.Marshal([]json.RawMessage{scopeRaw})
	if err != nil {
		return nil, err
	}
	resourceRaw, err := json.Marshal(resource)
	if err != nil {
		return nil, err
	}
	delete(root, "resource_metrics")
	root["resourceMetrics"], err = json.Marshal([]json.RawMessage{resourceRaw})
	if err != nil {
		return nil, err
	}
	return json.Marshal(root)
}
