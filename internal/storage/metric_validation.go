package storage

import (
	"context"
	"fmt"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func validateMetricEnvelope(ctx context.Context, data *metricpb.MetricsData) error {
	var walk func(protoreflect.Message) error
	walk = func(message protoreflect.Message) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var walkErr error
		message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
			if field.IsList() {
				list := value.List()
				if field.Kind() == protoreflect.MessageKind && field.Message().FullName() == "opentelemetry.proto.common.v1.KeyValue" {
					attrs := make([]*commonpb.KeyValue, 0, list.Len())
					for i := 0; i < list.Len(); i++ {
						if err := ctx.Err(); err != nil {
							walkErr = err
							return false
						}
						kv, ok := list.Get(i).Message().Interface().(*commonpb.KeyValue)
						if !ok || kv == nil {
							walkErr = fmt.Errorf("invalid attribute")
							return false
						}
						attrs = append(attrs, kv)
					}
					if err := canonicalAttrs(attrs, false); err != nil {
						walkErr = err
						return false
					}
				}
				for i := 0; i < list.Len(); i++ {
					if err := ctx.Err(); err != nil {
						walkErr = err
						return false
					}
					if field.Kind() == protoreflect.MessageKind {
						if err := walk(list.Get(i).Message()); err != nil {
							walkErr = err
							return false
						}
					}
					if field.Kind() == protoreflect.EnumKind && field.Enum().Values().ByNumber(list.Get(i).Enum()) == nil {
						walkErr = fmt.Errorf("unsupported enum value")
						return false
					}
				}
			} else if field.Kind() == protoreflect.MessageKind {
				if err := walk(value.Message()); err != nil {
					walkErr = err
					return false
				}
			} else if field.Kind() == protoreflect.EnumKind && field.Enum().Values().ByNumber(value.Enum()) == nil {
				walkErr = fmt.Errorf("unsupported enum value")
				return false
			}
			return true
		})
		return walkErr
	}
	if err := walk(data.ProtoReflect()); err != nil {
		return fmt.Errorf("indeterminate: malformed retained metrics envelope: %w", err)
	}
	for _, resource := range data.ResourceMetrics {
		if err := ctx.Err(); err != nil {
			return err
		}
		if resource == nil {
			return fmt.Errorf("indeterminate: missing resource envelope")
		}
		if resource.Resource != nil && len(resource.Resource.EntityRefs) > 0 {
			return fmt.Errorf("indeterminate: unsupported resource entity references")
		}
		for _, scope := range resource.ScopeMetrics {
			if scope == nil {
				return fmt.Errorf("indeterminate: missing scope envelope")
			}
			for _, metric := range scope.Metrics {
				if metric == nil || metric.Name == "" || metric.Data == nil {
					return fmt.Errorf("indeterminate: malformed metric")
				}
				if err := visitMetricPoints(metric, MetricSelector{}, func(metricPointAttributes, bool) error { return ctx.Err() }); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
