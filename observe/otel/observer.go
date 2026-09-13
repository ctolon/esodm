// Package otel provides an optional OpenTelemetry observer. It records operation
// duration and result status without collecting document contents or identifiers.
package otel

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ctolon/esodm"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Observer creates client spans using tracer without recording documents or IDs.
func Observer(tracer trace.Tracer) esodm.Observer {
	return func(ctx context.Context, event esodm.Event) {
		end := time.Now()
		operation := event.Operation
		if operation == "" {
			operation = "request"
		}
		_, span := tracer.Start(ctx, "elasticsearch "+operation,
			trace.WithTimestamp(end.Add(-event.Duration)),
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("db.system.name", "elasticsearch"),
				attribute.String("db.operation.name", operation),
				attribute.String("http.request.method", event.Method),
				attribute.Int("http.response.status_code", event.Status),
			),
		)
		if event.Index != "" {
			span.SetAttributes(attribute.String("db.namespace", event.Index))
		}
		if event.Err != nil {
			errorType := fmt.Sprintf("%T", event.Err)
			var serverError *esodm.Error
			if errors.As(event.Err, &serverError) && serverError.Type != "" {
				errorType = serverError.Type
			}
			span.SetAttributes(attribute.String("error.type", errorType))
			span.SetStatus(codes.Error, "Elasticsearch request failed")
		}
		span.End(trace.WithTimestamp(end))
	}
}
