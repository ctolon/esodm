# Production operations

## Connections

Reuse one official client, one connected `Client` and one repository per model for the lifetime of the process. Configure authentication, TLS, HTTP timeouts, compression and transport retries on the official client. Pass a context with a deadline to every call.

`Config.MaxResponseBytes` bounds the buffered response body; the default is 32 MiB and larger responses return `ErrResponseTooLarge`. Prefer smaller pages and source filters over a larger limit.

If cluster policy forbids `GET /`, `ConnectVersion(ctx, transport, version, config)` skips discovery and trusts the declared version. `DoTyped` still rejects a builder whose `compatible-with` header does not match the server major.

## Errors

| Error | Meaning |
| --- | --- |
| `ErrValidation` | Invalid input, or an HTTP 400 response |
| `ErrNotFound` | Missing document or resource (HTTP 404) |
| `ErrConflict` | Create conflict or optimistic concurrency failure (HTTP 409) |
| `ErrTooManyRequests` | HTTP 429 |
| `ErrUnavailable` | HTTP 502, 503 or 504 |
| `ErrUnsupported` | Unsupported server version, feature or client major |
| `ErrResponseTooLarge` | Response exceeded `MaxResponseBytes` |
| `ErrUnacknowledged` | Management action not acknowledged |
| `*Error` | Any non-2xx response, with `Status`, `Type`, `Reason`, `Body` and the official `Cause` |
| `*CommittedError` | Write succeeded, `AfterWrite` failed |
| `*BulkError` | One or more bulk items failed |
| `*PartialSearchError` | Timed-out or partially failed search |
| `*IncompleteOperationError` | By-query operation reported failures |
| `*MigrationPendingError` | Migration phase needs manual reconciliation |

Classify with `errors.Is` and inspect with `errors.As`. `Error.Retryable()` is true for 429 and 5xx gateway statuses; it says nothing about whether a write is safe to repeat.

`Error.Reason`, `Body` and `Cause` can contain document values. `Error` implements `slog.LogValuer` with only the status and type, and `Redacted()` returns a copy with the same fields, so structured loggers can record errors without the raw response.

## Telemetry

`Config.Observer` is called synchronously after every request with the operation name (`search`, `get`, `bulk`, `update_by_query` and so on), index, HTTP method, status, duration and error. Request metadata excludes bodies, IDs and query values; `Event.Err` may still contain sensitive server error details. Observers must be concurrency-safe and quick.

<!-- source: examples/guide/telemetry.go -->
```go
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
```

`observe/otel.Observer(tracer)` emits a client span per request named by operation with `db.system.name`, `db.operation.name`, `db.namespace`, HTTP method and status, and `error.type`. If the official client is configured with `elastictransport.Instrumentation`, esodm drives that instrumentation for every request, with a request copy that omits IDs, routing and credentials. Use one of the two span sources to avoid duplicate spans.

## Required privileges

| Operation | Privilege |
| --- | --- |
| `Connect` | Cluster `monitor` (for `GET /`), or use `ConnectVersion` |
| Reads, search, multi-get, PIT | Index `read` |
| Document writes and bulk | Index `write` |
| `EnsureIndex`, `CreateIndex` | Index `create_index` |
| `VerifyIndex`, `Mapping` | Index `view_index_metadata` |
| `PutMapping`, `PutSettings`, alias changes, migrations | Index `manage` on the indexes and aliases involved |
| Update and delete by query | Index `read` and `write` |
| Reindex | `read` on the source, `write` on the target |
| Task inspection and cancellation | Cluster `monitor` or `manage` |
| Templates, pipelines, ILM policies | Cluster `manage_index_templates`, `manage_pipeline`, `manage_ilm` |

Consult the [Elasticsearch privileges reference](https://www.elastic.co/docs/deploy-manage/users-roles/cluster-or-deployment-auth/elasticsearch-privileges) for the current names. Provisioning and migrations usually run under a separate administrative identity.

`scripts/secure.py --major 8` (or `9`) starts a TLS-enabled node, creates an API key limited to `esodm-secure-*` and runs the integration checks against it, including the assertion that another index is forbidden.

## Memory

Per request, the raw body is limited to `MaxResponseBytes` (plus one overflow-detection byte), while decoding, buffering allocations and decoded documents consume additional memory. This is not a process-wide or strict peak-memory limit. For `BulkStream` and `BulkSeq`, the operations held in memory are approximately `(QueueSize + Workers + 1) × BatchSize`, plus the encoded request bodies of active workers. Start with `BatchSize: 500`, `QueueSize: 2` and a small worker count, and raise concurrency according to cluster capacity.

## Shutdown

Close iterators, stop bulk producers and wait for `BulkStream` or `BulkSeq` to return before the process exits. Hooks and observers must not depend on goroutines that stop earlier.

## Testing application code

`esodmtest.NewClient(t, version)` returns a `Client` backed by a `Recorder`; no cluster is needed.

<!-- source: examples/testing/recorder_test.go -->
```go
// Package testingexample_test demonstrates testing application behavior without a cluster.
package testingexample_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ctolon/esodm"
	"github.com/ctolon/esodm/esodmtest"
	"github.com/ctolon/esodm/examples/guide"
)

func TestRepositoryRead(t *testing.T) {
	client, recorder := esodmtest.NewClient(t, esodm.Version{Major: 9, Minor: 5, Patch: 2})
	type product struct {
		Name string `json:"name"`
	}
	schema, err := esodm.NewSchema[product]("products")
	if err != nil {
		t.Fatal(err)
	}
	repo, err := esodm.NewRepository(client, schema)
	if err != nil {
		t.Fatal(err)
	}
	recorder.On("GET", "/products/_doc/1").Reply(200,
		`{"found":true,"_id":"1","_index":"products","_source":{"name":"Go"}}`)
	hit, err := repo.GetByID(context.Background(), "1")
	if err != nil {
		t.Fatal(err)
	}
	if hit.Source.Name != "Go" || hit.ID != "1" {
		t.Fatalf("unexpected product: %+v", hit)
	}
	recorder.AssertAll(t)
}

func TestApplicationHooks(t *testing.T) {
	client, recorder := esodmtest.NewClient(t, esodm.Version{Major: 9, Minor: 5, Patch: 2})
	schema, err := esodm.NewSchemaFor[guide.Product]()
	if err != nil {
		t.Fatal(err)
	}
	repo, err := esodm.NewRepository(client, schema, esodm.WithHooks(guide.ProductHooks()))
	if err != nil {
		t.Fatal(err)
	}
	// Application validation rejects the input before any HTTP request.
	if _, err := repo.Create(context.Background(), "invalid", guide.Product{Name: "  "}); !errors.Is(err, esodm.ErrValidation) {
		t.Fatalf("expected validation failure, got %v", err)
	}
	recorder.On("PUT", "/products/_create/1").Reply(201, `{"_id":"1","result":"created"}`)
	if _, err := repo.Create(context.Background(), "1", guide.Product{Name: "  Go  ", Price: 10}); err != nil {
		t.Fatal(err)
	}
	requests := recorder.Requests()
	if len(requests) != 1 {
		t.Fatalf("expected only the valid write, got %d requests", len(requests))
	}
	var stored guide.Product
	if err := json.Unmarshal(requests[0].Body, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Name != "Go" {
		t.Fatalf("normalization did not reach the request: %q", stored.Name)
	}
	recorder.AssertAll(t)
}
```

`On` matches the HTTP method and an anchored regular expression against the escaped path; each expectation is used once. The first unused matching expectation is selected, so different paths can arrive in any order. `Requests()` returns recorded requests with query strings, headers and bodies for assertions. Configure expectations before running code concurrently.

Run these application examples with `go test -v ./examples/testing`. They assert both returned values and the outgoing request, including the absence of a request after local validation failure.
