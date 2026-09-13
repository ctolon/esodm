package otel

import (
	"context"
	"errors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"strings"
	"testing"
	"time"

	"github.com/ctolon/esodm"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestObserver(t *testing.T) {
	observer := Observer(noop.NewTracerProvider().Tracer("test"))
	observer(context.Background(), esodm.Event{Method: "GET", Status: 200, Duration: time.Millisecond})
	observer(context.Background(), esodm.Event{Method: "POST", Status: 503, Err: errors.New("contains source data which must not be recorded")})
}

type recordingSpan struct {
	trace.Span
	attrs []attribute.KeyValue
}

func (s *recordingSpan) SetAttributes(attrs ...attribute.KeyValue) {
	s.attrs = append(s.attrs, attrs...)
}

type recordingTracer struct {
	trace.Tracer
	name string
	span *recordingSpan
}

func (t *recordingTracer) Start(ctx context.Context, name string, options ...trace.SpanStartOption) (context.Context, trace.Span) {
	t.name = name
	config := trace.NewSpanStartConfig(options...)
	t.span.attrs = append(t.span.attrs, config.Attributes()...)
	return ctx, t.span
}
func TestObserverAttributes(t *testing.T) {
	base := noop.NewTracerProvider().Tracer("test")
	_, span := base.Start(context.Background(), "base")
	recorder := &recordingTracer{Tracer: base, span: &recordingSpan{Span: span}}
	Observer(recorder)(context.Background(), esodm.Event{Method: "POST", Operation: "search", Index: "products", Status: 429, Err: &esodm.Error{Type: "rejected", Reason: "private value"}})
	if recorder.name != "elasticsearch search" {
		t.Fatal(recorder.name)
	}
	got := map[string]string{}
	for _, a := range recorder.span.attrs {
		got[string(a.Key)] = a.Value.Emit()
	}
	if got["db.operation.name"] != "search" || got["db.namespace"] != "products" || got["error.type"] != "rejected" {
		t.Fatal(got)
	}
	for _, value := range got {
		if strings.Contains(value, "private") {
			t.Fatal("sensitive error in telemetry")
		}
	}
}
