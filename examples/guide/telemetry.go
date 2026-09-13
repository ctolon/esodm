package guide

import (
	"context"
	"errors"
	"strconv"

	"github.com/ctolon/esodm"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// RequestMetrics records bounded operation/status labels without document identifiers.
// The retryable counter classifies responses; it does not count actual retry attempts.
func RequestMetrics(meter metric.Meter) (esodm.Observer, error) {
	duration, err := meter.Float64Histogram("esodm.request.duration", metric.WithUnit("s"), metric.WithDescription("Elasticsearch request duration"))
	if err != nil {
		return nil, err
	}
	retryable, err := meter.Int64Counter("esodm.request.retryable", metric.WithDescription("Responses classified as transient"))
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, event esodm.Event) {
		status := "transport_error"
		if event.Status > 0 {
			status = strconv.Itoa(event.Status/100) + "xx"
		}
		attrs := metric.WithAttributes(attribute.String("operation", event.Operation), attribute.String("status_class", status))
		duration.Record(ctx, event.Duration.Seconds(), attrs)
		var response *esodm.Error
		if errors.As(event.Err, &response) && response.Retryable() {
			retryable.Add(ctx, 1, attrs)
		}
	}, nil
}
