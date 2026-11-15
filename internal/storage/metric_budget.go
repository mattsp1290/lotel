package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

type metricJSONBudget struct {
	points    int
	tokens    int
	maxPoints int
}

func (budget *metricJSONBudget) token(decoder *json.Decoder) (json.Token, error) {
	budget.tokens++
	if budget.tokens > metricJSONTokenLimit {
		return nil, fmt.Errorf("indeterminate: metric JSON work limit exceeded")
	}
	return decoder.Token()
}

func (budget *metricJSONBudget) scanValue(decoder *json.Decoder, key string, depth int) error {
	if depth > metricJSONRecursionLimit {
		return fmt.Errorf("indeterminate: metric JSON nesting limit exceeded")
	}
	token, err := budget.token(decoder)
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		for decoder.More() {
			token, err := budget.token(decoder)
			if err != nil {
				return err
			}
			name, ok := token.(string)
			if !ok {
				return fmt.Errorf("indeterminate: invalid metric JSON object")
			}
			if err := budget.scanValue(decoder, name, depth+1); err != nil {
				return err
			}
		}
		end, err := budget.token(decoder)
		if err != nil || end != json.Delim('}') {
			return fmt.Errorf("indeterminate: invalid metric JSON object")
		}
	case '[':
		for decoder.More() {
			if key == "dataPoints" || key == "data_points" {
				budget.points++
				limit := budget.maxPoints
				if limit == 0 {
					limit = metricPointLimit
				}
				if budget.points > limit {
					return fmt.Errorf("indeterminate: metric point scan limit exceeded")
				}
			}
			if err := budget.scanValue(decoder, "", depth+1); err != nil {
				return err
			}
		}
		end, err := budget.token(decoder)
		if err != nil || end != json.Delim(']') {
			return fmt.Errorf("indeterminate: invalid metric JSON array")
		}
	default:
		return fmt.Errorf("indeterminate: invalid metric JSON delimiter")
	}
	return nil
}

func (budget *metricJSONBudget) scan(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := budget.scanValue(decoder, "", 0); err != nil {
		return fmt.Errorf("indeterminate: invalid or excessive metric JSON: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("indeterminate: trailing metric JSON")
	}
	return nil
}
