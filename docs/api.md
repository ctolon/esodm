# API reference

Generated from exported Go declarations. Run `go run ./internal/docgen` with Go 1.27 to update.

Start with the [documentation guide](README.md) for complete usage examples and operational contracts.

## github.com/ctolon/esodm

Package esodm maps Go documents to Elasticsearch using the official client.
All network operations require a context. Repositories are safe for concurrent
use after construction; documents remain owned by the caller. Writes are not
transactions across documents and search visibility follows Elasticsearch's
refresh policy.

### ErrUnavailable, ErrTooManyRequests, ErrUnacknowledged, ErrNotFound, ErrConflict, ErrValidation, ErrUnsupported, ErrResponseTooLarge

Values of ErrUnavailable, ErrTooManyRequests, ErrUnacknowledged, ErrNotFound, ErrConflict, ErrValidation, ErrUnsupported, ErrResponseTooLarge.

```go
var (
	// ErrUnavailable identifies transient gateway/service failures.
	ErrUnavailable = errors.New("esodm: unavailable")
	// ErrTooManyRequests identifies HTTP 429 rate limiting.
	ErrTooManyRequests = errors.New("esodm: too many requests")
	// ErrUnacknowledged reports an uncertain management outcome; inspect server state before retrying.
	ErrUnacknowledged = errors.New("esodm: management action not acknowledged; inspect server state before retrying")
	// ErrNotFound identifies a missing document or resource.
	ErrNotFound = errors.New("esodm: not found")
	// ErrConflict identifies an optimistic concurrency or create conflict.
	ErrConflict = errors.New("esodm: conflict")
	// ErrValidation identifies invalid input or an HTTP 400 response.
	ErrValidation = errors.New("esodm: validation")
	// ErrUnsupported identifies a server version or feature outside the supported contract.
	ErrUnsupported = errors.New("esodm: unsupported capability")
	// ErrResponseTooLarge indicates that the configured response byte limit was exceeded.
	ErrResponseTooLarge = errors.New("esodm: response exceeds configured limit")
)
```

### DecodeAgg

DecodeAgg decodes a named aggregation into T, returning ErrNotFound if absent.

```go
func DecodeAgg[T any](aggregations map[string]json.RawMessage, name string) (T, error)
```

### DecodeAggregation

DecodeAggregation decodes a named aggregation from a raw-document search result.

```go
func DecodeAggregation[T any](result SearchResult[json.RawMessage], name string) (T, error)
```

### DecodeFacet

DecodeFacet decodes the inner aggregation of a WithFacets result, with or without typed_keys.

```go
func DecodeFacet[A any](aggs map[string]json.RawMessage, name string) (A, error)
```

### EncodeCursor

EncodeCursor encodes a bounded cursor as URL-safe base64 JSON.
It is not signed or encrypted; add application authentication if needed.

```go
func EncodeCursor(c Cursor) (string, error)
```

### Admin

Admin exposes explicit resource management; repository construction never
creates or alters cluster resources implicitly.

```go
// Admin exposes explicit resource management; repository construction never
// creates or alters cluster resources implicitly.
type Admin struct {
	// contains filtered or unexported fields
}
```

### Admin.Aliases

Aliases applies all alias actions atomically and requires acknowledgement.

```go
func (a Admin) Aliases(ctx context.Context, actions ...AliasAction) error
```

### Admin.ApplyMigration

ApplyMigration requires the caller to pause application writes and serialize
migration/alias administration until completion. A failure retains both indices
for inspection and never deletes data. Re-plan with a new target before retrying.

```go
func (a Admin) ApplyMigration(ctx context.Context, plan *MigrationPlan, options MigrationOptions) (MigrationResult, error)
```

### Admin.CancelTask

CancelTask requests cancellation; callers must still inspect task completion.

```go
func (a Admin) CancelTask(ctx context.Context, id string) (taskcancel.Response, error)
```

### Admin.CreateIndex

CreateIndex creates an index using explicit mapping and settings values.
Both arguments accept official typed models; nil values omit those sections.

```go
func (a Admin) CreateIndex(ctx context.Context, name string, mapping, settings any) error
```

### Admin.Delete

Delete removes a named cluster resource and requires server acknowledgement.

```go
func (a Admin) Delete(ctx context.Context, kind ResourceKind, name string) error
```

### Admin.DeleteIndex

DeleteIndex permanently deletes the named index after validating its name.

```go
func (a Admin) DeleteIndex(ctx context.Context, name string) error
```

### Admin.ExplainLifecycle

ExplainLifecycle returns the index lifecycle status as raw JSON.

```go
func (a Admin) ExplainLifecycle(ctx context.Context, index string) (json.RawMessage, error)
```

### Admin.Get

Get returns the raw JSON representation of a named cluster resource.

```go
func (a Admin) Get(ctx context.Context, kind ResourceKind, name string) (json.RawMessage, error)
```

### Admin.Mapping

Mapping returns the raw mapping response for an index.

```go
func (a Admin) Mapping(ctx context.Context, index string) (json.RawMessage, error)
```

### Admin.Put

Put creates or replaces a named resource and requires server acknowledgement.
DataStream requires a nil spec; other kinds require a specification.

```go
func (a Admin) Put(ctx context.Context, kind ResourceKind, name string, spec any) error
```

### Admin.PutMapping

PutMapping applies an additive mapping update and requires acknowledgement.

```go
func (a Admin) PutMapping(ctx context.Context, index string, mapping any) error
```

### Admin.PutSettings

PutSettings updates index settings and requires acknowledgement.

```go
func (a Admin) PutSettings(ctx context.Context, index string, settings any) error
```

### Admin.Refresh

Refresh makes completed writes searchable and rejects failed shards.

```go
func (a Admin) Refresh(ctx context.Context, index string) error
```

### Admin.ResumeMigration

ResumeMigration waits for a recorded reindex, verifies mappings/counts and moves
the alias atomically. Cancellation leaves the server task running for later resume.
Creating/submitting phases require explicit reconciliation because task submission
and checkpoint persistence cannot form one transaction.

```go
func (a Admin) ResumeMigration(ctx context.Context, input MigrationState, o MigrationOptions) (MigrationState, error)
```

### Admin.Rollover

Rollover evaluates conditions and optionally creates the next write index.
When dryRun is true, the server only evaluates conditions.

```go
func (a Admin) Rollover(ctx context.Context, target string, conditions map[string]any, dryRun bool) (RolloverResult, error)
```

### Admin.SimulatePipeline

SimulatePipeline evaluates documents through a pipeline without indexing them.

```go
func (a Admin) SimulatePipeline(ctx context.Context, name string, documents []any) (json.RawMessage, error)
```

### Admin.StartMigration

StartMigration creates the target and submits an official asynchronous reindex.
Checkpoint is called before and after each side effect. A failed checkpoint stops
further work; the returned state always carries the latest known task/phase.

```go
func (a Admin) StartMigration(ctx context.Context, plan *MigrationPlan, o MigrationOptions) (MigrationState, error)
```

### Admin.Task

Task fetches a server task using the official task response model.

```go
func (a Admin) Task(ctx context.Context, id string) (taskget.Response, error)
```

### Admin.WaitTask

WaitTask polls until completion or cancellation. Cancellation does not cancel the server task.

```go
func (a Admin) WaitTask(ctx context.Context, id string, interval time.Duration) (taskget.Response, error)
```

### Aggregation

Aggregation supports arbitrary Elasticsearch metric, bucket and pipeline kinds.
Constructors snapshot parameters; unsupported server options are returned as errors.

```go
// Aggregation supports arbitrary Elasticsearch metric, bucket and pipeline kinds.
// Constructors snapshot parameters; unsupported server options are returned as errors.
type Aggregation struct {
	// contains filtered or unexported fields
}
```

### Agg

Agg snapshots an arbitrary aggregation kind and its child aggregations.
Prefer FromAggregation with official types for compile-time option checking.

```go
func Agg(kind string, params any, children map[string]Aggregation) Aggregation
```

### Avg

Avg constructs an avg metric aggregation using the official API.

```go
func Avg(field string) Aggregation
```

### Cardinality

Cardinality constructs a cardinality metric aggregation using the official API.

```go
func Cardinality(field string) Aggregation
```

### FromAggregation

FromAggregation snapshots an official aggregation or esdsl aggregation builder.

```go
func FromAggregation(value types.AggregationsVariant) Aggregation
```

### Max

Max constructs a max metric aggregation using the official API.

```go
func Max(field string) Aggregation
```

### MetricAgg

MetricAgg computes a field metric using an official model for known kinds.
Unknown kinds are passed through for server-side validation.

Deprecated: Use named metric constructors or FromAggregation.

```go
func MetricAgg(kind, field string) Aggregation
```

### Min

Min constructs a min metric aggregation using the official API.

```go
func Min(field string) Aggregation
```

### PipelineAgg

PipelineAgg is the dynamic escape hatch for pipeline aggregation options.
Prefer FromAggregation with an official pipeline builder for typed options.

```go
func PipelineAgg(kind string, paths any, script string) Aggregation
```

### Stats

Stats constructs a stats metric aggregation using the official API.

```go
func Stats(field string) Aggregation
```

### Sum

Sum constructs a sum metric aggregation using the official API.

```go
func Sum(field string) Aggregation
```

### TermsAgg

TermsAgg groups values into up to size term buckets with optional children.

```go
func TermsAgg(field string, size int, children map[string]Aggregation) Aggregation
```

### ValueCount

ValueCount constructs a value_count metric aggregation using the official API.

```go
func ValueCount(field string) Aggregation
```

### Aggregation.Err

Err returns a deferred aggregation construction error, including an empty snapshot.

```go
func (a Aggregation) Err() error
```

### Aggregation.MarshalJSON

MarshalJSON serializes the snapshot, returning any deferred construction error.

```go
func (a Aggregation) MarshalJSON() ([]byte, error)
```

### AliasAction

AliasAction adds or removes an index association in an atomic alias request.

```go
// AliasAction adds or removes an index association in an atomic alias request.
type AliasAction struct {
	// Add selects adding an alias association; false removes it.
	Add bool
	// Index is the required index associated with the alias.
	Index string
	// Alias is the required alias name.
	Alias string
	// WriteIndex sets is_write_index on add: nil omits it, true selects this index, false excludes
	// it as a write index. It is ignored on remove.
	WriteIndex *bool
}
```

### Assignment

Assignment is a snapshotted field value for NewPatch. Zero assignments are invalid.

```go
// Assignment is a snapshotted field value for NewPatch. Zero assignments are invalid.
type Assignment struct {
	// contains filtered or unexported fields
}
```

### BulkAction

BulkAction identifies an Elasticsearch bulk operation kind.

```go
// BulkAction identifies an Elasticsearch bulk operation kind.
type BulkAction string
```

### BulkIndex, BulkCreate, BulkUpdate, BulkDelete

Values of BulkIndex, BulkCreate, BulkUpdate, BulkDelete.

```go
const (
	// BulkIndex creates or replaces a complete document.
	BulkIndex BulkAction = "index"
	// BulkCreate creates a document only when its ID is absent.
	BulkCreate BulkAction = "create"
	// BulkUpdate merges a partial document patch.
	BulkUpdate BulkAction = "update"
	// BulkDelete removes a document.
	BulkDelete BulkAction = "delete"
)
```

### BulkBatchResult

BulkBatchResult associates an asynchronously completed batch with its input sequence.

```go
// BulkBatchResult associates an asynchronously completed batch with its input sequence.
type BulkBatchResult struct {
	// Sequence is the zero-based batch number assigned in producer order; delivery order may differ.
	Sequence int
	// Result contains known item outcomes for this batch.
	Result BulkResult
	// Err is the batch-level error, including BulkError for item failures; nil means the batch
	// succeeded.
	Err error
}
```

### BulkError

BulkError reports failed items; inspect BulkResult.Items for individual causes.

```go
// BulkError reports failed items; inspect BulkResult.Items for individual causes.
type BulkError struct {
	// Failed is the count of final failed items; inspect BulkResult.Items for their causes.
	Failed int
}
```

### *BulkError.Error

Error returns a human-readable description of the failure.

```go
func (e *BulkError) Error() string
```

### BulkItem

BulkItem reports one operation outcome, including hook or server errors.

```go
// BulkItem reports one operation outcome, including hook or server errors.
type BulkItem struct {
	// Action identifies the corresponding input operation.
	Action BulkAction
	WriteResult
	// Status is the per-item HTTP status; zero means no definitive server outcome was decoded.
	Status int
	// Err is the item failure or CommittedError; nil means success. Inspect alongside Status and
	// WriteResult.
	Err error
}
```

### BulkOperation

BulkOperation carries a document or patch and per-item write options.
Refresh must be set on the batch rather than on individual operations.

```go
// BulkOperation carries a document or patch and per-item write options.
// Refresh must be set on the batch rather than on individual operations.
type BulkOperation[T any] struct {
	// Action must be BulkIndex, BulkCreate, BulkUpdate or BulkDelete; the zero value is invalid.
	Action BulkAction
	// ID identifies the target; empty permits model ID inference for full documents and server-
	// generated IDs for BulkCreate only.
	ID string
	// Document is used by index/create; other actions ignore it. Preparation runs stamping and full-
	// document hooks.
	Document T
	// Patch is used by update; other actions ignore it. Missing fields are untouched and nil values
	// write JSON null.
	Patch Patch
	// Options supplies per-item routing, pipeline and concurrency tokens. Refresh is rejected here;
	// set it on the batch.
	Options WriteOptions
}
```

### BulkOptions

BulkOptions controls refresh and opt-in item retry.

```go
// BulkOptions controls refresh and opt-in item retry.
type BulkOptions struct {
	// Refresh accepts empty, false, true or wait_for and applies to each submitted batch, including
	// retries. Empty preserves the server default.
	Refresh Refresh
	// Retry enables opt-in retries of explicit 429/502/503/504 item failures; its zero value
	// disables retries. Transport failures are never retried by the ODM.
	Retry RetryPolicy
}
```

### BulkResult

BulkResult preserves request order and server processing time in milliseconds.

```go
// BulkResult preserves request order and server processing time in milliseconds.
type BulkResult struct {
	// Took is accumulated server processing time in milliseconds across attempts; it excludes client
	// backoff.
	Took int64
	// Items preserves input order. On request failure, prepared items can have unknown outcomes;
	// inspect the returned error before trusting results.
	Items []BulkItem
}
```

### BulkStreamOptions

BulkStreamOptions bounds batch size, worker count and queued batches.
Zero values select 500 items, one worker and two queued batches.

```go
// BulkStreamOptions bounds batch size, worker count and queued batches.
// Zero values select 500 items, one worker and two queued batches.
type BulkStreamOptions struct {
	// FlushInterval flushes partial batches periodically; zero disables the timer.
	FlushInterval time.Duration
	// BatchSize is the maximum operations per batch, 1..10000; zero selects 500. Each encoded batch
	// must also fit within 16 MiB.
	BatchSize int
	// Workers is the concurrent batch worker count, 1..64; zero selects one. More than one worker
	// allows out-of-order completion.
	Workers int
	// QueueSize is the number of queued batches, 1..64; zero selects two. It has no effect on
	// synchronous BulkSeq execution.
	QueueSize int
	// Refresh accepts empty, false, true or wait_for for each batch; empty preserves the server
	// default.
	Refresh Refresh
	// Retry applies only to item failures explicitly returned by Elasticsearch.
	Retry RetryPolicy
}
```

### ByQueryOptions

ByQueryOptions controls batch-by-query execution. Async returns a server task ID.
Conflicts defaults to abort. No per-document hooks or audit stamping run.

```go
// ByQueryOptions controls batch-by-query execution. Async returns a server task ID.
// Conflicts defaults to abort. No per-document hooks or audit stamping run.
type ByQueryOptions struct {
	// Routing limits shard routes; nil or empty omits it.
	Routing []string
	// Refresh refreshes affected shards when true; false leaves normal refresh scheduling. By-query
	// APIs do not accept wait_for.
	Refresh bool
	// Async returns a task ID instead of waiting when true; false waits for completion.
	Async bool
	// Conflicts accepts abort or proceed; an empty Name selects the server default. Proceed
	// tolerates version conflicts but does not make other failures successful.
	Conflicts conflicts.Conflicts
}
```

### Client

Client adds document mapping and bounded response handling to an official transport.
A connected Client may be shared across goroutines.

```go
// Client adds document mapping and bounded response handling to an official transport.
// A connected Client may be shared across goroutines.
type Client struct {
	// contains filtered or unexported fields
}
```

### Connect

Connect discovers the server version and verifies the supported baseline.

```go
func Connect(ctx context.Context, transport Transport, config Config) (*Client, error)
```

### ConnectVersion

ConnectVersion uses a caller-verified server version without a discovery request.
Use this when GET / is forbidden. Authentication and TLS remain transport concerns;
the declared version must match the cluster and official client major.

```go
func ConnectVersion(ctx context.Context, transport Transport, version Version, config Config) (*Client, error)
```

### *Client.Admin

Admin returns explicit cluster-resource management operations.

```go
func (c *Client) Admin() Admin
```

### *Client.Do

Do is the JSON escape hatch. path must be an absolute API path, never a URL.
No ODM retry is performed: configure retries only in the official transport.

```go
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body, out any) error
```

### *Client.DoTyped

DoTyped executes an official typed API builder through this client's transport,
response-size limit, error classification and observer. It decodes into out;
pass nil to discard the response. Builders are mutable and must not be shared
across concurrent calls. The builder's own transport is not used.

```go
func (c *Client) DoTyped(ctx context.Context, builder RequestBuilder, out any) error
```

### *Client.Load

Load is the Go 1.27 method form of Load.

Available with Go 1.27.

```go
func (c *Client) Load[T any](ctx context.Context, refs []Ref[T]) ([]ReferenceResult[T], error)
```

### *Client.Repository

Repository is the Go 1.27 method form of NewRepository.

Available with Go 1.27.

```go
func (c *Client) Repository[T any](schema *Schema[T], options ...RepositoryOption[T]) (*Repository[T], error)
```

### *Client.Transport

Transport returns the configured official client transport.

```go
func (c *Client) Transport() Transport
```

### *Client.Version

Version returns the server version discovered during Connect.

```go
func (c *Client) Version() Version
```

### CommittedError

CommittedError indicates that the write succeeded but its AfterWrite hook failed.
Retrying the write can repeat an already committed operation.

```go
// CommittedError indicates that the write succeeded but its AfterWrite hook failed.
// Retrying the write can repeat an already committed operation.
type CommittedError struct {
	// Result describes the successful write whose completion hook failed.
	Result WriteResult
	// Cause is the non-nil completion hook error, exposed through Unwrap.
	Cause error
}
```

### *CommittedError.Error

Error returns a human-readable description of the failure.

```go
func (e *CommittedError) Error() string
```

### *CommittedError.Unwrap

Unwrap returns the hook failure that followed the committed write.

```go
func (e *CommittedError) Unwrap() error
```

### Config

Config controls ODM response limits and optional request observation.

```go
// Config controls ODM response limits and optional request observation.
type Config struct {
	// Now supplies the audit clock; nil selects time.Now. Results are normalized to UTC.
	Now func() time.Time
	// MaxResponseBytes limits the raw response body to 1..1<<40 bytes; zero selects 32 MiB.
	// Decoded values and concurrent requests consume additional memory.
	MaxResponseBytes int64
	// Observer receives completion events; nil disables observation.
	Observer Observer
}
```

### CountOptions

CountOptions selects routing and shard preference for a count request.

```go
// CountOptions selects routing and shard preference for a count request.
type CountOptions struct {
	// Routing selects shard routes; nil or empty omits routing. Multiple values are comma-joined.
	Routing []string
	// Preference selects a server shard preference or custom session string; empty omits it.
	Preference string
}
```

### Cursor

Cursor is an opaque, bounded serialization helper, not an authorization token.
Callers exposing cursors to untrusted users must authenticate them separately.

```go
// Cursor is an opaque, bounded serialization helper, not an authorization token.
// Callers exposing cursors to untrusted users must authenticate them separately.
type Cursor struct {
	// PIT is the required live server PIT identifier. A cursor does not keep it alive automatically.
	PIT string `json:"pit"`
	// Sort contains the last delivered hit sort values, required and in sort order. Values remain
	// raw JSON to preserve numeric precision.
	Sort []json.RawMessage `json:"sort"`
}
```

### DecodeCursor

DecodeCursor decodes a bounded cursor and preserves numeric sort values.

```go
func DecodeCursor(s string) (Cursor, error)
```

### DateField

DateField supports range queries on date and date_nanos fields.

```go
// DateField supports range queries on date and date_nanos fields.
type DateField struct{ Field[time.Time] }
```

### Date

Date creates a descriptor whose values use time.Time JSON encoding.

```go
func Date(name string) DateField
```

### DateField.Between

Between matches an inclusive date interval.

```go
func (f DateField) Between(lo, hi time.Time) Query
```

### DateField.GT

GT matches dates after v.

```go
func (f DateField) GT(v time.Time) Query
```

### DateField.GTE

GTE matches dates on or after v.

```go
func (f DateField) GTE(v time.Time) Query
```

### DateField.LT

LT matches dates before v.

```go
func (f DateField) LT(v time.Time) Query
```

### DateField.LTE

LTE matches dates on or before v.

```go
func (f DateField) LTE(v time.Time) Query
```

### DocumentMeta

DocumentMeta provides opt-in ID and routing metadata excluded from _source.
Elasticsearch-generated IDs are returned in WriteResult, not written into caller values.

```go
// DocumentMeta provides opt-in ID and routing metadata excluded from _source.
// Elasticsearch-generated IDs are returned in WriteResult, not written into caller values.
type DocumentMeta struct {
	// ID is application metadata excluded from _source. Save requires a nonempty ID; Insert requires
	// it to be empty.
	ID string `json:"-"`
	// Routing is the default route for full-document writes; explicit nonempty write routing
	// overrides it.
	Routing string `json:"-"`
}
```

### DocumentMeta.DocumentID

DocumentID returns the model's ID.

```go
func (m DocumentMeta) DocumentID() string
```

### DocumentMeta.DocumentRouting

DocumentRouting returns the model's routing value.

```go
func (m DocumentMeta) DocumentRouting() string
```

### *DocumentMeta.SetDocumentMetadata

SetDocumentMetadata hydrates excluded ID and routing fields after repository reads.

```go
func (m *DocumentMeta) SetDocumentMetadata(metadata Metadata)
```

### Dynamic

Dynamic controls the mapping of previously unknown fields.

```go
// Dynamic controls the mapping of previously unknown fields.
type Dynamic string
```

### DynamicStrict, DynamicTrue, DynamicFalse, DynamicRuntime

Values of DynamicStrict, DynamicTrue, DynamicFalse, DynamicRuntime.

```go
const (
	// DynamicStrict rejects unmapped fields.
	DynamicStrict Dynamic = "strict"
	// DynamicTrue infers mappings for new fields.
	DynamicTrue Dynamic = "true"
	// DynamicFalse retains unknown source fields without indexing them.
	DynamicFalse Dynamic = "false"
	// DynamicRuntime creates runtime fields for new fields.
	DynamicRuntime Dynamic = "runtime"
)
```

### Ensurer

Ensurer describes a repository's index initialization contract.

```go
// Ensurer describes a repository's index initialization contract.
type Ensurer interface {
	Index() string
	EnsureIndex(context.Context) error
}
```

### Error

Error preserves the HTTP status and Elasticsearch error details.
Body may contain document data; avoid including it in unredacted logs.

```go
// Error preserves the HTTP status and Elasticsearch error details.
// Body may contain document data; avoid including it in unredacted logs.
type Error struct {
	// Cause preserves the official structured error, including root and nested causes.
	Cause types.ErrorCause
	// Status is the HTTP response status, including per-item bulk or multi-get statuses.
	Status int
	// Type is the Elasticsearch error type, or empty when unavailable.
	Type string
	// Reason is the server error description and can contain sensitive document data.
	Reason string
	// Body is the raw error response when available and can contain sensitive data; nil means it was
	// not retained.
	Body json.RawMessage
}
```

### *Error.Error

Error returns a human-readable description of the failure.

```go
func (e *Error) Error() string
```

### *Error.Is

Is classifies HTTP 400, 404 and 409 as validation, missing and conflict errors.

```go
func (e *Error) Is(target error) bool
```

### *Error.LogValue

LogValue returns safe structured fields. Reason, Body and Cause are omitted
because Elasticsearch may include document values in all three.

```go
func (e *Error) LogValue() slog.Value
```

### *Error.Redacted

Redacted returns a copy containing only HTTP status and error type.

```go
func (e *Error) Redacted() *Error
```

### *Error.Retryable

Retryable identifies transient HTTP statuses, not permission to replay a write.
A transport failure or timed-out write may already have committed.

```go
func (e *Error) Retryable() bool
```

### Event

Event omits request bodies and credentials from its metadata. Err may contain
sensitive server response details. Observers must be concurrency-safe and prompt.

```go
// Event omits request bodies and credentials from its metadata. Err may contain
// sensitive server response details. Observers must be concurrency-safe and prompt.
type Event struct {
	// Operation identifies the endpoint without document IDs or query values.
	Operation string
	// Index identifies the target index when present in the request path.
	Index string
	// Method is the request HTTP method.
	Method string
	// Status is the response HTTP status; zero means no response status was available.
	Status int
	// Duration measures the transport and response processing time, including transport retries.
	Duration time.Duration
	// Err is the request error and may contain sensitive server details. Use Error.Redacted or safe
	// structured logging.
	Err error
}
```

### Facet

Facet combines a bucket/metric aggregation with an optional selected-value filter.
Within a facet, combine multiple selected values with Or or Terms as appropriate.

```go
// Facet combines a bucket/metric aggregation with an optional selected-value filter.
// Within a facet, combine multiple selected values with Or or Terms as appropriate.
type Facet struct {
	// Aggregation defines the facet buckets or metric; it must be a valid aggregation.
	Aggregation Aggregation
	// Selection is this facet selection filter; a zero Query means no selection. Its own aggregation
	// excludes this selection while applying other selections.
	Selection Query
}
```

### Field

Field binds a field name to its Go value type for query construction.

```go
// Field binds a field name to its Go value type for query construction.
type Field[T any] struct {
	// contains filtered or unexported fields
}
```

### ChildField

ChildField creates a typed child descriptor without generic methods (Go 1.26 compatible).

```go
func ChildField[T any](parent ObjectField, name string) Field[T]
```

### NewField

NewField creates a typed descriptor for an Elasticsearch field name.

```go
func NewField[T any](name string) Field[T]
```

### Field[T].Asc

Asc sorts this field in ascending order.

```go
func (f Field[T]) Asc() Sort
```

### Field[T].Clear

Clear writes JSON null; it does not remove the field from _source.

```go
func (f Field[T]) Clear() Assignment
```

### Field[T].Desc

Desc sorts this field in descending order.

```go
func (f Field[T]) Desc() Sort
```

### Field[T].Eq

Eq matches the field against an exact value.

```go
func (f Field[T]) Eq(value T) Query
```

### Field[T].Exists

Exists matches documents with an indexed value for the field.

```go
func (f Field[T]) Exists() Query
```

### Field[T].In

In matches any supplied field value; an empty list matches nothing.

```go
func (f Field[T]) In(values ...T) Query
```

### Field[T].Name

Name returns the Elasticsearch field name.

```go
func (f Field[T]) Name() string
```

### Field[T].Set

Set snapshots a value matching this field's type.

```go
func (f Field[T]) Set(value T) Assignment
```

### FieldMapping

FieldMapping describes a mapping property, including nested objects and vectors.

```go
// FieldMapping describes a mapping property, including nested objects and vectors.
type FieldMapping struct {
	// DocValues enables on-disk columnar values; nil uses the field type default. Some types,
	// including text, do not support this option.
	DocValues *bool `json:"doc_values,omitempty"`
	// IgnoreAbove is a nonnegative string length threshold; nil omits it. Elasticsearch validates
	// support for the selected type.
	IgnoreAbove *int `json:"ignore_above,omitempty"`
	// NullValue is a JSON scalar replacement for explicit null; nil omits it. It does not replace
	// missing fields or change _source.
	NullValue json.RawMessage `json:"null_value,omitempty"`
	// CopyTo lists destination field names; nil or empty omits it. Values are copied for indexing,
	// not into _source.
	CopyTo []string `json:"copy_to,omitempty"`
	// Dynamic controls unmapped child fields: strict, true, false or runtime. Nil inherits the
	// enclosing mapping behavior.
	Dynamic *dynamicmapping.DynamicMapping `json:"dynamic,omitempty"`
	// InferenceID names an existing inference endpoint for semantic_text; empty omits it. Endpoint
	// availability and permissions are server concerns.
	InferenceID string `json:"inference_id,omitempty"`
	// Type is the Elasticsearch mapping type, such as keyword, text, long, object, nested, join or
	// dense_vector. Inferred schemas require a nonempty type; the server validates type names.
	Type string `json:"type,omitempty"`
	// Analyzer selects a built-in or index-configured indexing analyzer; empty uses the server
	// default for the type.
	Analyzer string `json:"analyzer,omitempty"`
	// SearchAnalyzer selects the search-time analyzer; empty leaves the server default unchanged.
	SearchAnalyzer string `json:"search_analyzer,omitempty"`
	// Format is an Elasticsearch date format or ||-separated alternatives; empty uses the field type
	// default. It is not a Go time layout.
	Format string `json:"format,omitempty"`
	// Properties defines object or nested child mappings. Nil or empty omits children; inferred
	// schemas reject mapping depth greater than 32.
	Properties map[string]FieldMapping `json:"properties,omitempty"`
	// Fields defines named multi-fields, such as a keyword subfield of text. Nil or empty omits
	// multi-fields.
	Fields map[string]FieldMapping `json:"fields,omitempty"`
	// Dims is required for inferred dense_vector mappings and must be 1..4096. Zero omits the
	// property for other types.
	Dims int `json:"dims,omitempty"`
	// Similarity selects a server-supported similarity: dense vectors commonly use cosine,
	// dot_product, l2_norm or max_inner_product; text can name a configured similarity. Empty uses
	// the server default.
	Similarity string `json:"similarity,omitempty"`
	// Index controls indexing of the field; nil uses the field type default. False does not remove
	// values from _source.
	Index *bool `json:"index,omitempty"`
	// Relations maps parent names to a child string, []string or []any of strings. Required for a
	// root join field; names must be nonempty and a child must differ from its parent.
	Relations map[string]any `json:"relations,omitempty"`
}
```

### Finder

Finder is an immutable repository-bound search. Copies can be reused concurrently.
All returns one search window; Each traverses all matching documents using a PIT.

```go
// Finder is an immutable repository-bound search. Copies can be reused concurrently.
// All returns one search window; Each traverses all matching documents using a PIT.
type Finder[T any] struct {
	// contains filtered or unexported fields
}
```

### Finder[T].Aggregate

Aggregate adds or replaces a named aggregation, retaining existing aggregations.

```go
func (f Finder[T]) Aggregate(name string, a Aggregation) Finder[T]
```

### Finder[T].All

All returns hits from one search window, preserving partial results on failure.

```go
func (f Finder[T]) All(ctx context.Context) ([]Hit[T], error)
```

### Finder[T].Collapse

Collapse groups hits by a field.

```go
func (f Finder[T]) Collapse(field string) Finder[T]
```

### Finder[T].Count

Count counts query matches, ignoring pagination, sorting, aggregations and KNN.
Routing and preference are preserved.

```go
func (f Finder[T]) Count(ctx context.Context) (int64, error)
```

### Finder[T].Each

Each traverses matching hits using a PIT with a one-minute keep-alive.
Stopping iteration closes the PIT; cleanup failures can only be yielded while the consumer continues.

```go
func (f Finder[T]) Each(ctx context.Context, pageSize int) iter.Seq2[Hit[T], error]
```

### Finder[T].EachSource

EachSource traverses matching source documents, closing the PIT on termination.

```go
func (f Finder[T]) EachSource(ctx context.Context, pageSize int) iter.Seq2[T, error]
```

### Finder[T].Err

Err returns construction errors, including an unbound repository.

```go
func (f Finder[T]) Err() error
```

### Finder[T].Exists

Exists reports whether the configured query matches a document. It ignores the
offset and size, requests one hit and stops collecting after one match per shard.

```go
func (f Finder[T]) Exists(ctx context.Context) (bool, error)
```

### Finder[T].Filter

Filter adds required clauses without score contributions.

```go
func (f Finder[T]) Filter(q ...Query) Finder[T]
```

### Finder[T].First

First returns the first hit at the configured offset, overriding Size with one.
A successful empty search returns ErrNotFound.

```go
func (f Finder[T]) First(ctx context.Context) (Hit[T], error)
```

### Finder[T].From

From sets the search offset.

```go
func (f Finder[T]) From(n int) Finder[T]
```

### Finder[T].Highlight

Highlight selects fields for highlighting.

```go
func (f Finder[T]) Highlight(fields ...string) Finder[T]
```

### Finder[T].KNN

KNN configures a nearest-neighbor search.

```go
func (f Finder[T]) KNN(k KNN) Finder[T]
```

### Finder[T].Must

Must adds required scoring clauses.

```go
func (f Finder[T]) Must(q ...Query) Finder[T]
```

### Finder[T].Not

Not excludes documents matching any supplied clause.

```go
func (f Finder[T]) Not(q ...Query) Finder[T]
```

### Finder[T].Page

Page retrieves a numbered window. Deep pagination is limited by the index's
max_result_window; use Each for traversal beyond that limit.

```go
func (f Finder[T]) Page(ctx context.Context, page, size int) (Page[T], error)
```

### Finder[T].Preference

Preference sets shard preference.

```go
func (f Finder[T]) Preference(p string) Finder[T]
```

### Finder[T].Result

Result executes the search, preserving partial results and their error.

```go
func (f Finder[T]) Result(ctx context.Context) (SearchResult[T], error)
```

### Finder[T].Routing

Routing limits the search to the supplied routing values.

```go
func (f Finder[T]) Routing(values ...string) Finder[T]
```

### Finder[T].Search

Search returns the compiled snapshot for use with other repository operations.

```go
func (f Finder[T]) Search() Search
```

### Finder[T].Should

Should adds optional scoring clauses. Without required clauses, at least one must match.

```go
func (f Finder[T]) Should(q ...Query) Finder[T]
```

### Finder[T].Size

Size sets the maximum number of hits in a search window.

```go
func (f Finder[T]) Size(n int) Finder[T]
```

### Finder[T].Sort

Sort replaces the search sort order.

```go
func (f Finder[T]) Sort(s ...Sort) Finder[T]
```

### Finder[T].Source

Source selects source fields to include and exclude.

```go
func (f Finder[T]) Source(includes, excludes []string) Finder[T]
```

### Finder[T].Sources

Sources returns source documents from one search window.

```go
func (f Finder[T]) Sources(ctx context.Context) ([]T, error)
```

### Finder[T].With

With snapshots an additional search body option.

```go
func (f Finder[T]) With(key string, value any) Finder[T]
```

### GeoField

GeoField provides geo query wrappers around official request types.

```go
// GeoField provides geo query wrappers around official request types.
type GeoField struct{ Field[GeoPoint] }
```

### Geo

Geo creates a descriptor for a geo_point mapping.

```go
func Geo(name string) GeoField
```

### GeoField.WithinBox

WithinBox selects points inside official geo bounds.

```go
func (f GeoField) WithinBox(bounds types.GeoBounds) Query
```

### GeoField.WithinDistance

WithinDistance selects points within an Elasticsearch distance, such as "5km".

```go
func (f GeoField) WithinDistance(distance string, point types.GeoLocation) Query
```

### GeoPoint

GeoPoint is the official latitude/longitude source representation.

```go
// GeoPoint is the official latitude/longitude source representation.
type GeoPoint = types.LatLonGeoLocation
```

### Graph

Graph contains loaded documents and missing references from explicit preloading.

```go
// Graph contains loaded documents and missing references from explicit preloading.
type Graph[T any] struct {
	// Nodes holds distinct loaded references in breadth-first order, including missing or failed
	// items.
	Nodes []ReferenceResult[T]
	// Truncated reports unvisited references beyond MaxDepth; it does not mean MaxDocuments was
	// exceeded.
	Truncated bool
}
```

### Preload

Preload traverses explicitly supplied references breadth-first. It never mutates
source documents. Repeated references/cycles are loaded once. Use separate calls
for heterogeneous target types. Truncated signals the depth limit was reached.

```go
func Preload[T any](ctx context.Context, client *Client, roots []Ref[T], children func(T) []Ref[T], options PreloadOptions) (Graph[T], error)
```

### Hit

Hit pairs a decoded source document with search or get metadata.

```go
// Hit pairs a decoded source document with search or get metadata.
type Hit[T any] struct {
	Metadata
	// Source is the decoded document. When _source is omitted or filtered it may be zero-valued or
	// incomplete; do not blindly replace the stored document with it.
	Source T `json:"_source"`
	// Score is the relevance score; nil means unscored or unavailable.
	Score *float64 `json:"_score,omitempty"`
	// Sort holds ordered raw JSON sort values for search_after. Nil means no sort values were
	// returned.
	Sort []json.RawMessage `json:"sort,omitempty"`
	// Highlight maps field names to highlighted fragments; nil means none were returned. Treat
	// fragments as untrusted text when rendering HTML.
	Highlight map[string][]string `json:"highlight,omitempty"`
	// InnerHits holds named raw inner-hit groups; use InnerHits to decode a group into its document
	// type.
	InnerHits map[string]json.RawMessage `json:"inner_hits,omitempty"`
	// Fields holds requested stored, runtime or doc-value fields as raw JSON; nil means none were
	// returned.
	Fields map[string]json.RawMessage `json:"fields,omitempty"`
}
```

### Hooks

Hooks execute synchronously. AfterWrite errors are marked as committed, so a
caller can avoid retrying an already successful write. Hooks must be thread safe.

```go
// Hooks execute synchronously. AfterWrite errors are marked as committed, so a
// caller can avoid retrying an already successful write. Hooks must be thread safe.
type Hooks[T any] struct {
	// Validate checks a prepared full document or upsert create candidate before sending; nil
	// disables it. Failures wrap ErrValidation.
	Validate func(context.Context, *T) error
	// BeforeWrite runs after stamping for full-document writes; nil disables it. An error prevents
	// sending the operation.
	BeforeWrite func(context.Context, Operation, *T) error
	// AfterWrite runs after a successful single or bulk item write; nil disables it. An error
	// becomes CommittedError, not permission to replay.
	AfterWrite func(context.Context, Operation, WriteResult) error
	// AfterRead runs after source decoding and metadata hydration on repository reads; nil disables
	// it. An error is returned with the affected result.
	AfterRead func(context.Context, *Hit[T]) error
	// BeforePatch receives a shallow copy of the patch before update or bulk update.
	// Nested caller-owned values must not be mutated by the hook.
	BeforePatch func(context.Context, string, Patch) error
	// BeforeDelete runs before single or bulk deletion.
	BeforeDelete func(context.Context, string) error
}
```

### Identified

Identified supplies an explicit document ID for Save and full-document bulk operations.

```go
// Identified supplies an explicit document ID for Save and full-document bulk operations.
type Identified interface{ DocumentID() string }
```

### IncompleteOperationError

IncompleteOperationError indicates server-reported partial work, which is not rolled back.

```go
// IncompleteOperationError indicates server-reported partial work, which is not rolled back.
type IncompleteOperationError struct {
	// Operation identifies the incomplete server operation, such as reindex or update_by_query.
	Operation string
}
```

### *IncompleteOperationError.Error

Error describes a non-atomic operation's incomplete outcome.

```go
func (e *IncompleteOperationError) Error() string
```

### Indexed

Indexed declares a model's index or alias. IndexName must work on a zero model.

```go
// Indexed declares a model's index or alias. IndexName must work on a zero model.
type Indexed interface{ IndexName() string }
```

### IndexerOperation

IndexerOperation is a prepared, owned payload for official esutil adapters.
Complete must be called exactly once for the final item outcome.

```go
// IndexerOperation is a prepared, owned payload for official esutil adapters.
// Complete must be called exactly once for the final item outcome.
type IndexerOperation struct {
	// Action is the normalized bulk operation kind.
	Action BulkAction
	// Index, ID and Routing are the prepared target metadata. Empty ID requests generation for create.
	Index, ID, Routing string
	// IfSeqNo and IfPrimaryTerm are paired optional concurrency tokens.
	IfSeqNo, IfPrimaryTerm *int64
	// Body is the owned JSON payload without a trailing newline; delete operations have no body.
	Body []byte
	// Complete must be called exactly once with the final result, HTTP status and error. It applies
	// routing and invokes AfterWrite on success.
	Complete func(context.Context, WriteResult, int, error) BulkItem
}
```

### Iterator

Iterator owns one PIT. It is not safe for concurrent use. Always defer Close,
even when stopping before exhaustion. Cancellation and exhaustion also close it.

```go
// Iterator owns one PIT. It is not safe for concurrent use. Always defer Close,
// even when stopping before exhaustion. Cancellation and exhaustion also close it.
type Iterator[T any] struct {
	// contains filtered or unexported fields
}
```

### *Iterator[T].Close

Close releases the PIT using an independent timeout and is safe to call repeatedly.

```go
func (it *Iterator[T]) Close() error
```

### *Iterator[T].Cursor

Cursor snapshots the latest PIT and the last emitted hit, never unread page hits.
The iterator retains ownership until Detach succeeds.

```go
func (it *Iterator[T]) Cursor() (Cursor, error)
```

### *Iterator[T].Detach

Detach transfers PIT cleanup responsibility to the caller and stops iteration.
ResumeIterator takes ownership again; otherwise close the PIT before expiry.

```go
func (it *Iterator[T]) Detach() (Cursor, error)
```

### *Iterator[T].Each

Each yields remaining hits and closes the PIT, including on early termination.
Cleanup errors are available through Err when the consumer stops early.

```go
func (it *Iterator[T]) Each() iter.Seq2[Hit[T], error]
```

### *Iterator[T].Err

Err returns the iteration or automatic PIT cleanup error, if any.

```go
func (it *Iterator[T]) Err() error
```

### *Iterator[T].Hit

Hit returns the current hit; it is valid after Next returns true.

```go
func (it *Iterator[T]) Hit() Hit[T]
```

### *Iterator[T].Next

Next advances to the next hit, closing the PIT on exhaustion or failure.
Call Err after it returns false.

```go
func (it *Iterator[T]) Next() bool
```

### Join

Join identifies a parent or child relation; children must include a parent ID
and use consistent routing for every document operation.

```go
// Join identifies a parent or child relation; children must include a parent ID
// and use consistent routing for every document operation.
type Join struct {
	// Name is a declared parent or child relation name.
	Name string `json:"name"`
	// Parent is required for child relations and must be a valid document ID; root parent documents
	// leave it empty.
	Parent string `json:"parent,omitempty"`
}
```

### KNN

KNN describes a vector search with an optional filter. K must be positive
and Candidates must be between K and 10,000.

```go
// KNN describes a vector search with an optional filter. K must be positive
// and Candidates must be between K and 10,000.
type KNN struct {
	// Field is a required dense_vector field name.
	Field string `json:"field"`
	// Vector is a nonempty query vector of finite values. Its dimension and similarity constraints
	// must match the server mapping.
	Vector []float32 `json:"query_vector"`
	// K is the positive requested nearest-neighbor count; it must not exceed Candidates.
	K int `json:"k"`
	// Candidates is the per-shard candidate count, K..10000; zero is invalid.
	Candidates int `json:"num_candidates"`
	// Filter optionally restricts candidate documents; nil applies no additional filter. Search.KNN
	// snapshots it.
	Filter *Query `json:"filter,omitempty"`
}
```

### MappingDrift

MappingDrift compares the expected mapping with one existing index's mapping.
Fields are reported at the top level, so nested differences identify their parent.
ChangedOptions covers expected top-level mapping options other than properties.

```go
// MappingDrift compares the expected mapping with one existing index's mapping.
// Fields are reported at the top level, so nested differences identify their parent.
// ChangedOptions covers expected top-level mapping options other than properties.
type MappingDrift struct {
	// Index is the inspected concrete index or alias.
	Index string
	// ChangedFields lists differing properties present in both schema and server.
	ChangedFields []string
	// MissingFields lists schema properties absent from the server mapping.
	MissingFields []string
	// ExtraFields lists server properties absent from the schema.
	ExtraFields []string
	// ChangedOptions lists differing top-level mapping options outside properties.
	ChangedOptions []string
}
```

### MappingDrift.Matches

Matches reports whether the server mapping matches the schema's expectations.

```go
func (d MappingDrift) Matches() bool
```

### Metadata

Metadata identifies a document and carries optimistic concurrency tokens.

```go
// Metadata identifies a document and carries optimistic concurrency tokens.
type Metadata struct {
	// ID is the server document identifier. Explicit write IDs must be valid UTF-8 and 1..512 bytes.
	ID string `json:"_id"`
	// Index is the concrete index reported by Elasticsearch, which can differ from a repository
	// alias.
	Index string `json:"_index"`
	// Routing is the resolved route, or empty when unavailable or unused.
	Routing string `json:"_routing,omitempty"`
	// SeqNo is the nonnegative sequence number; nil means it was not returned. Use with PrimaryTerm
	// for optimistic concurrency.
	SeqNo *int64 `json:"_seq_no,omitempty"`
	// PrimaryTerm is the positive primary term; nil means it was not returned.
	PrimaryTerm *int64 `json:"_primary_term,omitempty"`
	// Version is the server document version; zero means unavailable.
	Version int64 `json:"_version,omitempty"`
}
```

### Metadata.Conditional

Conditional returns independent concurrency tokens and routing from this metadata.

```go
func (m Metadata) Conditional() WriteOptions
```

### MetadataReceiver

MetadataReceiver opts source models into hydration of Elasticsearch metadata.

```go
// MetadataReceiver opts source models into hydration of Elasticsearch metadata.
type MetadataReceiver interface{ SetDocumentMetadata(Metadata) }
```

### MigrationOptions

MigrationOptions records the caller's assurance that application writes are paused.

```go
// MigrationOptions records the caller's assurance that application writes are paused.
type MigrationOptions struct {
	// WritesPaused must be true. It asserts that the caller paused writers and serialized migrations
	// and alias administration; the library does not acquire a lock.
	WritesPaused bool
	// PollInterval is a nonnegative asynchronous polling duration; zero selects one second.
	// ApplyMigration does not poll, but still rejects a negative value.
	PollInterval time.Duration
	// Checkpoint persists asynchronous progress. It must complete durably before returning nil.
	Checkpoint func(MigrationState) error
}
```

### MigrationPendingError

MigrationPendingError means a side effect may have occurred before its acknowledgement
was saved. Inspect the cluster and repair the trusted checkpoint; do not restart blindly.

```go
// MigrationPendingError means a side effect may have occurred before its acknowledgement
// was saved. Inspect the cluster and repair the trusted checkpoint; do not restart blindly.
type MigrationPendingError struct {
	// Phase is the ambiguous phase, normally creating or submitting.
	Phase string
}
```

### *MigrationPendingError.Error

Error describes an ambiguous migration checkpoint.

```go
func (e *MigrationPendingError) Error() string
```

### MigrationPlan

MigrationPlan owns snapshots; public Summary returns a copy. Plans must be
prepared again after any source mapping or alias change.

```go
// MigrationPlan owns snapshots; public Summary returns a copy. Plans must be
// prepared again after any source mapping or alias change.
type MigrationPlan struct {
	// contains filtered or unexported fields
}
```

### PlanMigration

PlanMigration snapshots a single-index alias and desired schema without writing.
Filtered or routed aliases are rejected; target must name a new index.

```go
func PlanMigration[T any](ctx context.Context, client *Client, alias, target string, schema *Schema[T]) (*MigrationPlan, error)
```

### *MigrationPlan.MarshalJSON

MarshalJSON serializes the snapshot, returning any deferred construction error.

```go
func (p *MigrationPlan) MarshalJSON() ([]byte, error)
```

### *MigrationPlan.Summary

Summary returns an independent copy of the migration plan description.

```go
func (p *MigrationPlan) Summary() MigrationSummary
```

### MigrationResult

MigrationResult reports the retained indices, copied count and alias switch outcome.

```go
// MigrationResult reports the retained indices, copied count and alias switch outcome.
type MigrationResult struct {
	// Source is the retained original index.
	Source string
	// Target is the destination index; it is retained even when migration fails.
	Target string
	// Documents is the server-reported number of created documents.
	Documents int64
	// AliasMoved reports successful acknowledgement of the atomic alias switch.
	AliasMoved bool
}
```

### MigrationState

MigrationState is a serializable checkpoint for one explicitly authorized migration.
Store it in trusted application storage; it is not an authenticated capability.
Application writes and competing alias administration must remain paused until completion.

```go
// MigrationState is a serializable checkpoint for one explicitly authorized migration.
// Store it in trusted application storage; it is not an authenticated capability.
// Application writes and competing alias administration must remain paused until completion.
type MigrationState struct {
	// Version is the checkpoint format version; only 1 is supported.
	Version int `json:"version"`
	// Summary records the approved source, target, alias and planned changes.
	Summary MigrationSummary `json:"summary"`
	// Mapping is the desired target mapping, normalized from the server after creation. It must be
	// valid JSON.
	Mapping json.RawMessage `json:"mapping"`
	// Settings is the snapshotted target settings JSON; JSON null means unspecified settings.
	Settings json.RawMessage `json:"settings"`
	// SourceMapping is the original source mapping snapshot, checked for drift before alias
	// switching.
	SourceMapping json.RawMessage `json:"source_mapping"`
	// Phase is planned, creating, created, submitting, copying, switching or complete. Planned
	// states cannot be resumed; creating and submitting require manual reconciliation.
	Phase string `json:"phase"`
	// TaskID is the asynchronous reindex task identifier; required in copying phase.
	TaskID string `json:"task_id,omitempty"`
	// Documents is the verified nonnegative copied count; zero before task completion is not proof
	// of an empty source.
	Documents int64 `json:"documents"`
}
```

### MigrationSummary

MigrationSummary lists the source, target, mapping differences and planned steps.

```go
// MigrationSummary lists the source, target, mapping differences and planned steps.
type MigrationSummary struct {
	// Alias is the application alias to switch.
	Alias string `json:"alias"`
	// Source is the concrete index currently selected by Alias.
	Source string `json:"source"`
	// Target is the new concrete index to create.
	Target string `json:"target"`
	// ChangedFields lists top-level mapping properties that differ, including additions and
	// removals.
	ChangedFields []string `json:"changed_fields"`
	// Steps lists planned operations in execution order; this is descriptive output, not executable
	// instructions.
	Steps []string `json:"steps"`
	// ChangedMappingOptions lists differences outside the properties map.
	ChangedMappingOptions []string `json:"changed_mapping_options"`
	// TargetSettings contains the exact desired settings; server defaults are not compared.
	TargetSettings json.RawMessage `json:"target_settings"`
}
```

### MultiSearchResult

MultiSearchResult pairs an individual result with its item-level error.

```go
// MultiSearchResult pairs an individual result with its item-level error.
type MultiSearchResult[T any] struct {
	// Result contains the search response, possibly partial when Err is non-nil.
	Result SearchResult[T]
	// Err is this search response error; the outer MSearch error instead reports a whole-request
	// failure.
	Err error
}
```

### ObjectField

ObjectField groups queries and field paths for an object or nested mapping.

```go
// ObjectField groups queries and field paths for an object or nested mapping.
type ObjectField struct{ Field[any] }
```

### Object

Object creates an object path descriptor.

```go
func Object(name string) ObjectField
```

### ObjectField.Nested

Nested wraps a query in this descriptor's nested path.

```go
func (f ObjectField) Nested(query Query) Query
```

### Observer

Observer receives request completion events synchronously and must be concurrency-safe.

```go
// Observer receives request completion events synchronously and must be concurrency-safe.
type Observer func(context.Context, Event)
```

### Operation

Operation identifies a document lifecycle operation, including server-assigned IDs.

```go
// Operation identifies a document lifecycle operation, including server-assigned IDs.
type Operation = BulkAction
```

### OpInsert, OpCreate, OpIndex, OpUpdate, OpDelete

Values of OpInsert, OpCreate, OpIndex, OpUpdate, OpDelete.

```go
const (
	// OpInsert creates a document with a server-generated ID.
	OpInsert Operation = "insert"
	// OpCreate creates a document with an explicit ID.
	OpCreate Operation = "create"
	// OpIndex replaces a document with an explicit ID.
	OpIndex Operation = "index"
	// OpUpdate updates a document.
	OpUpdate Operation = "update"
	// OpDelete deletes a document.
	OpDelete Operation = "delete"
)
```

### Ordered

Ordered permits numeric and string field values in range queries.

```go
// Ordered permits numeric and string field values in range queries.
type Ordered interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~float32 | ~float64 | ~string
}
```

### OrderedField

OrderedField adds range comparisons to a typed field descriptor.

```go
// OrderedField adds range comparisons to a typed field descriptor.
type OrderedField[T Ordered] struct {
	Field[T]
}
```

### OrderedValue

OrderedValue creates a descriptor supporting range comparisons.

```go
func OrderedValue[T Ordered](name string) OrderedField[T]
```

### OrderedField[T].Between

Between matches values in the inclusive range [lo, hi].

```go
func (f OrderedField[T]) Between(lo, hi T) Query
```

### OrderedField[T].GT

GT matches field values strictly greater than v.

```go
func (f OrderedField[T]) GT(v T) Query
```

### OrderedField[T].GTE

GTE matches field values greater than or equal to v.

```go
func (f OrderedField[T]) GTE(v T) Query
```

### OrderedField[T].LT

LT matches field values strictly less than v.

```go
func (f OrderedField[T]) LT(v T) Query
```

### OrderedField[T].LTE

LTE matches field values less than or equal to v.

```go
func (f OrderedField[T]) LTE(v T) Query
```

### Page

Page is a one-based search window with an exact or lower-bound document total.
HasNext compares the returned window with that total; it is conservative when Exact is false.

```go
// Page is a one-based search window with an exact or lower-bound document total.
// HasNext compares the returned window with that total; it is conservative when Exact is false.
type Page[T any] struct {
	// Hits contains the requested page in server sort order.
	Hits []Hit[T]
	// Number is the one-based requested page number.
	Number int
	// Size is the requested page size.
	Size int
	// Total is the reported hit count; Exact indicates whether it is a lower bound.
	Total int64
	// Exact is true only when Elasticsearch reports an exact total.
	Exact bool
	// HasNext indicates more results according to the returned count or lower bound; it is not a
	// snapshot guarantee.
	HasNext bool
}
```

### Page[T].Sources

Sources returns the source documents in this page.

```go
func (p Page[T]) Sources() []T
```

### PartialSearchError

PartialSearchError reports a timeout or failed shards in a search-like response.

```go
// PartialSearchError reports a timeout or failed shards in a search-like response.
type PartialSearchError struct {
	// TimedOut is true when the server reported a search timeout.
	TimedOut bool
	// FailedShards is the number of failed shards reported by the server.
	FailedShards int
}
```

### *PartialSearchError.Error

Error returns a human-readable description of the failure.

```go
func (e *PartialSearchError) Error() string
```

### Patch

Patch contains only fields to change. Missing keys are untouched; nil writes
JSON null. This deliberately does not use a zero-valued document as a patch.

```go
// Patch contains only fields to change. Missing keys are untouched; nil writes
// JSON null. This deliberately does not use a zero-valued document as a patch.
type Patch map[string]any
```

### NewPatch

NewPatch builds a partial document, rejecting invalid or duplicate field names.
Dotted names are rejected: Elasticsearch doc patches require nested objects.

```go
func NewPatch(assignments ...Assignment) (Patch, error)
```

### PreloadOptions

PreloadOptions bounds graph traversal depth and number of distinct documents.

```go
// PreloadOptions bounds graph traversal depth and number of distinct documents.
type PreloadOptions struct {
	// MaxDepth bounds breadth-first levels including roots, 1..32; zero selects three. Unvisited
	// references at the limit set Graph.Truncated.
	MaxDepth int
	// MaxDocuments limits distinct references including missing documents; zero selects 10000 and
	// negative values are invalid. Exceeding it returns the partial graph and ErrValidation.
	MaxDocuments int
}
```

### Query

Query owns its serialized representation. Building from mutable caller values
snapshots them immediately; a Query can be reused across goroutines.

```go
// Query owns its serialized representation. Building from mutable caller values
// snapshots them immediately; a Query can be reused across goroutines.
type Query struct {
	// contains filtered or unexported fields
}
```

### And

And requires every query to match; an empty list matches all documents.

```go
func And(queries ...Query) Query
```

### Exists

Exists matches documents with an indexed value for the field.

```go
func Exists(field string) Query
```

### Filter

Filter requires every query to match without contributing to the score.

```go
func Filter(queries ...Query) Query
```

### FromQuery

FromQuery snapshots an official v9 typed query or esdsl builder. The query is
serialized immediately; subsequent builder mutations do not affect the snapshot.
Only clauses supported by the connected server should be used with ES 8.

```go
func FromQuery(value types.QueryVariant) Query
```

### GeoShape

GeoShape snapshots a complete official geo_shape query.

```go
func GeoShape(query types.GeoShapeQuery) Query
```

### HasChild

HasChild matches parents with a child of kind matching q.

```go
func HasChild(kind string, q Query) Query
```

### HasParent

HasParent matches children whose parent of kind matches q.

```go
func HasParent(kind string, q Query) Query
```

### IDs

IDs matches document IDs; an empty list matches nothing.

```go
func IDs(ids ...string) Query
```

### Match

Match analyzes text and matches it against the field.

```go
func Match(field, text string) Query
```

### MatchAll

MatchAll matches every document.

```go
func MatchAll() Query
```

### MatchNone

MatchNone matches no documents.

```go
func MatchNone() Query
```

### MatchPhrase

MatchPhrase matches analyzed terms in phrase order.

```go
func MatchPhrase(field, text string) Query
```

### MultiMatch

MultiMatch analyzes text across the supplied fields.

```go
func MultiMatch(text string, fields ...string) Query
```

### Nested

Nested evaluates q against nested documents at path without scoring.

```go
func Nested(path string, q Query) Query
```

### Not

Not excludes documents matching any supplied query.

```go
func Not(queries ...Query) Query
```

### Or

Or requires at least one query to match; an empty list matches nothing.

```go
func Or(queries ...Query) Query
```

### ParentID

ParentID matches children of kind belonging to the given parent ID.

```go
func ParentID(kind, id string) Query
```

### Percolate

Percolate snapshots an official percolate query. Exactly one document source is required.

```go
func Percolate(query types.PercolateQuery) Query
```

### Prefix

Prefix matches indexed terms beginning with prefix.

```go
func Prefix(field, prefix string) Query
```

### RawQuery

RawQuery snapshots a JSON query containing exactly one root clause.

```go
func RawQuery(raw json.RawMessage) Query
```

### Semantic

Semantic searches a semantic_text field through the official query builder.
The mapping and inference endpoint must be configured on the server.

```go
func Semantic(field, text string) Query
```

### SparseVector

SparseVector creates a query from precomputed sparse-vector tokens.

```go
func SparseVector(field string, tokens map[string]float32) Query
```

### Term

Term matches an exact field value using the official term query model.

```go
func Term[T any](field string, value T) Query
```

### Terms

Terms matches any supplied exact value; an empty list matches nothing.

```go
func Terms[T any](field string, values ...T) Query
```

### Wildcard

Wildcard matches indexed terms against an Elasticsearch wildcard pattern.

```go
func Wildcard(field, pattern string) Query
```

### Query.Err

Err returns a deferred query construction error.

```go
func (q Query) Err() error
```

### Query.MarshalJSON

MarshalJSON serializes the snapshot, returning any deferred construction error.

```go
func (q Query) MarshalJSON() ([]byte, error)
```

### ReadOptions

ReadOptions selects routing, shard preference, realtime behavior and source fields.
A nil Realtime or Source leaves the server default unchanged.

```go
// ReadOptions selects routing, shard preference, realtime behavior and source fields.
// A nil Realtime or Source leaves the server default unchanged.
type ReadOptions struct {
	// Routing must match the indexing route; empty omits routing. Join repositories require an
	// explicit route.
	Routing string
	// Preference is an Elasticsearch shard preference, such as _local or a custom session string;
	// empty uses the server default.
	Preference string
	// Realtime selects realtime GET behavior; nil omits the parameter, false explicitly disables it
	// and true explicitly enables it.
	Realtime *bool
	// Source selects Includes and Excludes field patterns. Nil or empty lists leave source filtering
	// unchanged; exclusions win over inclusions. Source filtering does not change the stored
	// document.
	Source *types.SourceFilter
}
```

### Ref

Ref stores a document reference, not a copy of the target. T denotes its Go
source type. Routing is required when the referenced document is routed.

```go
// Ref stores a document reference, not a copy of the target. T denotes its Go
// source type. Routing is required when the referenced document is routed.
type Ref[T any] struct {
	// Index is the target local index or alias, required independently of T.
	Index string `json:"index"`
	// ID is the target document ID, valid UTF-8 and 1..512 bytes.
	ID string `json:"id"`
	// Routing must match the target indexing route; empty omits it.
	Routing string `json:"routing,omitempty"`
}
```

### ReferenceResult

ReferenceResult associates a reference with its loaded hit or individual error.

```go
// ReferenceResult associates a reference with its loaded hit or individual error.
type ReferenceResult[T any] struct {
	// Ref is the original input reference.
	Ref Ref[T]
	// Hit contains the decoded target when Found is true. Duplicate references may share nested maps
	// and pointers.
	Hit Hit[T]
	// Found distinguishes an existing document from a missing or failed item.
	Found bool
	// Err is an individual server or repository read-hook error. A missing document has Found=false
	// and Err=nil.
	Err error
}
```

### Load

Load batches and deduplicates references. Results retain input order, including
duplicate and missing references. Per-document errors are retained in results.

```go
func Load[T any](ctx context.Context, client *Client, refs []Ref[T]) ([]ReferenceResult[T], error)
```

### Refresh

Refresh controls visibility of completed document and bulk writes.

```go
// Refresh controls visibility of completed document and bulk writes.
type Refresh string
```

### RefreshFalse, RefreshTrue, RefreshWaitFor

Values of RefreshFalse, RefreshTrue, RefreshWaitFor.

```go
const (
	// RefreshFalse leaves visibility to the normal refresh schedule.
	RefreshFalse Refresh = "false"
	// RefreshTrue refreshes affected shards immediately.
	RefreshTrue Refresh = "true"
	// RefreshWaitFor waits for a scheduled refresh.
	RefreshWaitFor Refresh = "wait_for"
)
```

### Registry

Registry explicitly groups repositories for sequential, non-migrating initialization.
Its zero value is ready to use. Add and snapshot operations are concurrency safe.

```go
// Registry explicitly groups repositories for sequential, non-migrating initialization.
// Its zero value is ready to use. Add and snapshot operations are concurrency safe.
type Registry struct {
	// contains filtered or unexported fields
}
```

### *Registry.Add

Add atomically registers repositories, rejecting nil entries and duplicate index names.

```go
func (g *Registry) Add(entries ...Ensurer) error
```

### *Registry.EnsureAll

EnsureAll initializes each index, stopping at the first error. It never migrates existing mappings.

```go
func (g *Registry) EnsureAll(ctx context.Context) error
```

### *Registry.Indexes

Indexes returns registration-order index names in an independent slice.

```go
func (g *Registry) Indexes() []string
```

### *Registry.VerifyAll

VerifyAll checks registered repositories in registration order without changing
mappings. A registered Ensurer lacking VerifyIndex is rejected before requests.

```go
func (g *Registry) VerifyAll(ctx context.Context) ([]MappingDrift, error)
```

### RelationField

RelationField is an optional typed descriptor for explicit reference loading.

```go
// RelationField is an optional typed descriptor for explicit reference loading.
type RelationField[T any] struct {
	Field[Ref[T]]
}
```

### Relation

Relation creates a typed field descriptor for explicit reference loading.

```go
func Relation[T any](name string) RelationField[T]
```

### RelationField[T].Load

Load resolves references using the client and preserves input order.

```go
func (f RelationField[T]) Load(ctx context.Context, client *Client, refs ...Ref[T]) ([]ReferenceResult[T], error)
```

### Repository

Repository maps documents of T using an immutable schema and optional hooks.

```go
// Repository maps documents of T using an immutable schema and optional hooks.
type Repository[T any] struct {
	// contains filtered or unexported fields
}
```

### NewRepository

NewRepository binds a client and schema without creating cluster resources.
Options are applied in order; a later hook configuration replaces earlier hooks.

```go
func NewRepository[T any](client *Client, schema *Schema[T], options ...RepositoryOption[T]) (*Repository[T], error)
```

### *Repository[T].Bulk

Bulk submits at most 10,000 operations and 16 MiB of encoded NDJSON.
Failed items are reported individually and are not retried by the ODM.

```go
func (r *Repository[T]) Bulk(ctx context.Context, ops []BulkOperation[T], refresh Refresh) (BulkResult, error)
```

### *Repository[T].BulkSeq

BulkSeq consumes a sequence in bounded batches. Multiple workers or a flush
interval select BulkStream scheduling; otherwise consumption is synchronous.
Producers must stop when yield returns false and honor ctx during blocking work.
Result callbacks are serial; parallel batches may complete out of input order.

```go
func (r *Repository[T]) BulkSeq(ctx context.Context, seq iter.Seq[BulkOperation[T]], options BulkStreamOptions, onResult func(BulkBatchResult) error) error
```

### *Repository[T].BulkSeq2

BulkSeq2 accepts a fallible producer, including a transformation of Repository.Each.
Accepted buffered operations are flushed before returning a producer error.

```go
func (r *Repository[T]) BulkSeq2(ctx context.Context, seq iter.Seq2[BulkOperation[T], error], options BulkStreamOptions, onResult func(BulkBatchResult) error) error
```

### *Repository[T].BulkStream

BulkStream consumes until input closes, flushing the last batch. The bounded
batch queue provides backpressure. Results may arrive out of order and include
their sequence number. onResult is invoked serially. Returning an error cancels
pending work; callers must stop their producer when ctx is canceled. Failed
items are delivered; Retry explicitly enables transient item retries. A non-nil batch error is also
returned after all accepted batches finish.

```go
func (r *Repository[T]) BulkStream(ctx context.Context, input <-chan BulkOperation[T], options BulkStreamOptions, onResult func(BulkBatchResult) error) error
```

### *Repository[T].BulkWithOptions

BulkWithOptions prepares each document once, then retries only explicit transient
item failures. Successful items and AfterWrite failures are never replayed.
Output order always matches input. Context/transport failures return known results plus an error.

```go
func (r *Repository[T]) BulkWithOptions(ctx context.Context, ops []BulkOperation[T], options BulkOptions) (BulkResult, error)
```

### *Repository[T].Count

Count returns the matching document count and rejects partial shard results.

```go
func (r *Repository[T]) Count(ctx context.Context, q Query) (int64, error)
```

### *Repository[T].CountWith

CountWith counts matching documents with explicit request options.

```go
func (r *Repository[T]) CountWith(ctx context.Context, q Query, options CountOptions) (int64, error)
```

### *Repository[T].Create

Create inserts a document only if its ID is absent, otherwise returning ErrConflict.

```go
func (r *Repository[T]) Create(ctx context.Context, id string, doc T, opts ...WriteOption) (WriteResult, error)
```

### *Repository[T].Delete

Delete removes a document, optionally checking optimistic concurrency tokens.

```go
func (r *Repository[T]) Delete(ctx context.Context, id string, opts ...WriteOption) (WriteResult, error)
```

### *Repository[T].DeleteByQuery

DeleteByQuery executes an explicit query through the official builder.
Deleted documents are not restored on partial failure; per-document hooks do not run.

```go
func (r *Repository[T]) DeleteByQuery(ctx context.Context, req *deletebyquery.Request, o ByQueryOptions) (deletebyquery.Response, error)
```

### *Repository[T].Each

Each opens and owns a PIT for the duration of a range-over-function loop.

```go
func (r *Repository[T]) Each(ctx context.Context, search Search, pageSize int, keepAlive string) iter.Seq2[Hit[T], error]
```

### *Repository[T].EncodeOperation

EncodeOperation runs pre-write hooks and validation and encodes one operation
for an external bulk indexer. Returned lines exclude trailing newlines.
It does not send a request or invoke AfterWrite; the external indexer owns results.

```go
func (r *Repository[T]) EncodeOperation(ctx context.Context, op BulkOperation[T]) (metadata, body []byte, err error)
```

### *Repository[T].EnsureIndex

EnsureIndex creates the index if absent; it does not migrate existing mappings.

```go
func (r *Repository[T]) EnsureIndex(ctx context.Context) error
```

### *Repository[T].Exists

Exists reports whether a document exists using an HTTP HEAD request.

```go
func (r *Repository[T]) Exists(ctx context.Context, id, routing string) (bool, error)
```

### *Repository[T].ExistsWith

ExistsWith checks document existence with routing and consistency options.
Source filtering has no effect on a HEAD request.

```go
func (r *Repository[T]) ExistsWith(ctx context.Context, id string, options ReadOptions) (bool, error)
```

### *Repository[T].Find

Find starts a search requiring all supplied queries. No queries matches everything.

```go
func (r *Repository[T]) Find(q ...Query) Finder[T]
```

### *Repository[T].FindSearch

FindSearch continues an existing immutable search, retaining its options and query.

```go
func (r *Repository[T]) FindSearch(s Search) Finder[T]
```

### *Repository[T].Get

Get loads one document and invokes AfterRead. Missing documents return ErrNotFound.

```go
func (r *Repository[T]) Get(ctx context.Context, id, routing string) (Hit[T], error)
```

### *Repository[T].GetByID

GetByID loads an unrouted document. Use Get when explicit routing is required.

```go
func (r *Repository[T]) GetByID(ctx context.Context, id string) (Hit[T], error)
```

### *Repository[T].GetWith

GetWith loads a document with explicit routing, source and consistency options.

```go
func (r *Repository[T]) GetWith(ctx context.Context, id string, options ReadOptions) (Hit[T], error)
```

### *Repository[T].Index

Index returns the repository index or alias name.

```go
func (r *Repository[T]) Index() string
```

### *Repository[T].Insert

Insert creates a document with a server-generated ID. Concurrency tokens are invalid.

```go
func (r *Repository[T]) Insert(ctx context.Context, doc T, opts ...WriteOption) (WriteResult, error)
```

### *Repository[T].Iterate

Iterate opens a PIT and returns an iterator that owns pagination and cleanup.
An empty keep-alive selects one minute; pageSize must be between 1 and 10,000.

```go
func (r *Repository[T]) Iterate(ctx context.Context, search Search, pageSize int, keepAlive string) (*Iterator[T], error)
```

### *Repository[T].MGet

MGet loads document IDs in input order, preserving missing and failed items.

```go
func (r *Repository[T]) MGet(ctx context.Context, ids []string, routing string) ([]ReferenceResult[T], error)
```

### *Repository[T].MGetWith

MGetWith loads IDs in input order with explicit routing and read options.

```go
func (r *Repository[T]) MGetWith(ctx context.Context, ids []string, options ReadOptions) ([]ReferenceResult[T], error)
```

### *Repository[T].MultiSearch

MultiSearch executes searches in one request and preserves their order.
Request errors are returned separately from individual search errors.

```go
func (r *Repository[T]) MultiSearch(ctx context.Context, searches ...Search) ([]MultiSearchResult[T], error)
```

### *Repository[T].PrepareIndexerOperation

PrepareIndexerOperation runs preparation once. Per-item pipelines and refresh
are rejected because esutil.BulkIndexerItem cannot represent them.

```go
func (r *Repository[T]) PrepareIndexerOperation(ctx context.Context, op BulkOperation[T]) (IndexerOperation, error)
```

### *Repository[T].Replace

Replace indexes the complete source, creating the document if absent.
Supply both concurrency tokens to protect a read-modify-write operation.

```go
func (r *Repository[T]) Replace(ctx context.Context, id string, doc T, opts ...WriteOption) (WriteResult, error)
```

### *Repository[T].ResumeIterator

ResumeIterator owns an existing PIT. Supply the same query and sort used to
produce the cursor; changing either invalidates pagination semantics.

```go
func (r *Repository[T]) ResumeIterator(ctx context.Context, search Search, cursor Cursor, pageSize int, keepAlive string) (*Iterator[T], error)
```

### *Repository[T].Save

Save indexes a model using DocumentID and optional DocumentRouting.

```go
func (r *Repository[T]) Save(ctx context.Context, doc T, opts ...WriteOption) (WriteResult, error)
```

### *Repository[T].Schema

Schema returns the repository's immutable schema.

```go
func (r *Repository[T]) Schema() *Schema[T]
```

### *Repository[T].Search

Search executes a typed document search and runs AfterRead for returned hits.
It returns PartialSearchError when the server reports incomplete results.

```go
func (r *Repository[T]) Search(ctx context.Context, search Search) (SearchResult[T], error)
```

### *Repository[T].Sources

Sources yields only source documents and iteration errors, closing its PIT on exit.

```go
func (r *Repository[T]) Sources(ctx context.Context, search Search, pageSize int, keepAlive string) iter.Seq2[T, error]
```

### *Repository[T].Update

Update merges explicitly supplied fields into an existing document.
Full-document validation hooks are not run for a partial patch.

```go
func (r *Repository[T]) Update(ctx context.Context, id string, patch Patch, opts ...WriteOption) (WriteResult, error)
```

### *Repository[T].UpdateByQuery

UpdateByQuery executes the official request, preserving its complete response.
An explicit query is required; use MatchAll intentionally for all documents.

```go
func (r *Repository[T]) UpdateByQuery(ctx context.Context, req *updatebyquery.Request, o ByQueryOptions) (updatebyquery.Response, error)
```

### *Repository[T].UpdateWith

UpdateWith executes an official update body against this repository's index.
Doc patches run BeforePatch and audit stamping; scripts execute on the server
and must implement their own audit/validation policy. AfterWrite runs on success.

```go
func (r *Repository[T]) UpdateWith(ctx context.Context, id string, request *update.Request, options UpdateOptions) (UpdateResult[T], error)
```

### *Repository[T].Upsert

Upsert applies patch when the document exists or inserts create when absent.
The create document is validated before sending the request.

```go
func (r *Repository[T]) Upsert(ctx context.Context, id string, patch Patch, create T, opts ...WriteOption) (WriteResult, error)
```

### *Repository[T].VerifyIndex

VerifyIndex reads and compares mappings without changing cluster state.
The repository target must resolve to exactly one index; use concrete repositories
to verify each index behind a multi-index alias or search pattern.

```go
func (r *Repository[T]) VerifyIndex(ctx context.Context) (MappingDrift, error)
```

### *Repository[T].WithIndex

WithIndex derives a repository sharing the client, hooks and mapping snapshot.
A concrete name supports reads and writes. Wildcards, lists and remote targets
support Search, MultiSearch, Count and PIT iteration; document writes reject them.

```go
func (r *Repository[T]) WithIndex(name string) (*Repository[T], error)
```

### RepositoryOption

RepositoryOption configures repository lifecycle behavior.

```go
// RepositoryOption configures repository lifecycle behavior.
type RepositoryOption[T any] interface {
	// contains filtered or unexported methods
}
```

### WithHooks

WithHooks replaces lifecycle hooks. A later WithHooks replaces the entire set.

```go
func WithHooks[T any](hooks Hooks[T]) RepositoryOption[T]
```

### RequestBuilder

RequestBuilder is implemented by official v8 and v9 typed API builders.
Use a builder from the same client major as the connected server.

```go
// RequestBuilder is implemented by official v8 and v9 typed API builders.
// Use a builder from the same client major as the connected server.
type RequestBuilder interface {
	HttpRequest(context.Context) (*http.Request, error)
}
```

### ResourceKind

ResourceKind selects one of the resource endpoints supported by Admin.

```go
// ResourceKind selects one of the resource endpoints supported by Admin.
type ResourceKind string
```

### IndexTemplate, ComponentTemplate, IngestPipeline, LifecyclePolicy, DataStream

Values of IndexTemplate, ComponentTemplate, IngestPipeline, LifecyclePolicy, DataStream.

```go
const (
	// IndexTemplate selects composable index templates.
	IndexTemplate ResourceKind = "_index_template"
	// ComponentTemplate selects reusable template components.
	ComponentTemplate ResourceKind = "_component_template"
	// IngestPipeline selects document ingest pipelines.
	IngestPipeline ResourceKind = "_ingest/pipeline"
	// LifecyclePolicy selects index lifecycle policies.
	LifecyclePolicy ResourceKind = "_ilm/policy"
	// DataStream selects data stream resources.
	DataStream ResourceKind = "_data_stream"
)
```

### RetryPolicy

RetryPolicy enables bounded, jittered retries of explicit 429/502/503/504 item failures.
Zero MaxRetries disables retries. Whole-request/ambiguous transport failures are never retried by the ODM.

```go
// RetryPolicy enables bounded, jittered retries of explicit 429/502/503/504 item failures.
// Zero MaxRetries disables retries. Whole-request/ambiguous transport failures are never retried by the ODM.
type RetryPolicy struct {
	// MaxRetries is the number of additional attempts, from 0 to 20. Zero disables ODM item retries.
	MaxRetries int
	// InitialBackoff is a nonnegative initial delay; zero selects 100ms. When both delays are
	// explicit it must not exceed MaxBackoff.
	InitialBackoff time.Duration
	// MaxBackoff is a nonnegative delay cap; zero selects 30s. Each retry waits in the upper half of
	// the exponentially increasing capped delay, with jitter.
	MaxBackoff time.Duration
}
```

### RolloverResult

RolloverResult reports condition checks and the indices involved in rollover.

```go
// RolloverResult reports condition checks and the indices involved in rollover.
type RolloverResult struct {
	// OldIndex is the previous write index.
	OldIndex string `json:"old_index"`
	// NewIndex is the proposed or created next write index.
	NewIndex string `json:"new_index"`
	// RolledOver reports whether the server performed the rollover.
	RolledOver bool `json:"rolled_over"`
	// DryRun reports that only conditions were evaluated.
	DryRun bool `json:"dry_run"`
	// Conditions maps server condition descriptions to whether each condition matched.
	Conditions map[string]bool `json:"conditions"`
}
```

### Routed

Routed supplies default routing when write options do not specify it.

```go
// Routed supplies default routing when write options do not specify it.
type Routed interface{ DocumentRouting() string }
```

### Schema

Schema is an immutable document mapping and index configuration for T.

```go
// Schema is an immutable document mapping and index configuration for T.
type Schema[T any] struct {
	// contains filtered or unexported fields
}
```

### NewSchema

NewSchema infers a mapping from a struct and its json/es tags.
It defaults to strict dynamic mapping and rejects recursive embedded objects.

```go
func NewSchema[T any](index string, config ...SchemaOption) (*Schema[T], error)
```

### NewSchemaFor

NewSchemaFor reads IndexName from a zero T or *T, then infers its mapping.

```go
func NewSchemaFor[T any](config ...SchemaOption) (*Schema[T], error)
```

### SchemaFromMapping

SchemaFromMapping creates a schema from the official mapping and settings
types without reflection-based field inference. It snapshots the inputs and
lets Elasticsearch validate mapping options. T must be a struct. Use this
constructor for mappings beyond the tag-based convenience API.

```go
func SchemaFromMapping[T any](index string, mapping *types.TypeMapping, settings *types.IndexSettings) (*Schema[T], error)
```

### *Schema[T].Index

Index returns the index or alias used by repositories built from the schema.

```go
func (s *Schema[T]) Index() string
```

### *Schema[T].Mapping

Mapping returns a copy of the serialized Elasticsearch type mapping.

```go
func (s *Schema[T]) Mapping() json.RawMessage
```

### *Schema[T].Settings

Settings returns a copy of the serialized index settings, or JSON null.

```go
func (s *Schema[T]) Settings() json.RawMessage
```

### SchemaConfig

SchemaConfig controls tag-based inference and explicit mapping overrides.
Use SchemaFromMapping for the complete official mapping API.

```go
// SchemaConfig controls tag-based inference and explicit mapping overrides.
// Use SchemaFromMapping for the complete official mapping API.
type SchemaConfig struct {
	// Properties overrides inferred mappings. An explicit conflicting es tag is an error.
	Properties map[string]FieldMapping
	// Settings contains JSON-serializable Elasticsearch index settings. Nil serializes as null and
	// omits settings during index creation; NewSchema snapshots the map.
	Settings map[string]any
	// Dynamic accepts strict, true, false or runtime; empty defaults to DynamicStrict.
	Dynamic Dynamic
}
```

### SchemaOption

SchemaOption configures an inferred schema. Options are applied in order.
SchemaConfig replaces the complete configuration; later options override it.

```go
// SchemaOption configures an inferred schema. Options are applied in order.
// SchemaConfig replaces the complete configuration; later options override it.
type SchemaOption interface {
	// contains filtered or unexported methods
}
```

### WithDynamic

WithDynamic selects how Elasticsearch handles unmapped fields.

```go
func WithDynamic(dynamic Dynamic) SchemaOption
```

### WithProperties

WithProperties replaces explicit mapping overrides. NewSchema snapshots the map.

```go
func WithProperties(properties map[string]FieldMapping) SchemaOption
```

### WithSettings

WithSettings replaces index settings. NewSchema snapshots the map.

```go
func WithSettings(settings map[string]any) SchemaOption
```

### Search

Search is an immutable snapshot of a search body, safe for concurrent reuse.

```go
// Search is an immutable snapshot of a search body, safe for concurrent reuse.
type Search struct {
	// contains filtered or unexported fields
}
```

### NewSearch

NewSearch creates a search with exact hit counts and concurrency metadata enabled.

```go
func NewSearch(q Query) Search
```

### SearchFromRequest

SearchFromRequest snapshots an official search request, preserving explicitly
supplied options. It adds ODM metadata defaults only when they are absent.

```go
func SearchFromRequest(request *typedsearch.Request) Search
```

### WithFacets

WithFacets builds disjunctive facets: hits use all selections, while each facet
counts values using every selection except its own. The base query still scopes
both hits and counts. Facet names must not collide with existing aggregations.
An existing post_filter is rejected because it would make count semantics ambiguous.

```go
func WithFacets(base Search, facets map[string]Facet) Search
```

### Search.Aggregations

Aggregations sets named aggregation snapshots on the search.

```go
func (s Search) Aggregations(a map[string]Aggregation) Search
```

### Search.Collapse

Collapse groups returned hits by a field value.

```go
func (s Search) Collapse(field string) Search
```

### Search.Err

Err returns a deferred construction error without executing a request.

```go
func (s Search) Err() error
```

### Search.From

From sets the result offset; use PIT iteration for deep pagination.

```go
func (s Search) From(n int) Search
```

### Search.Highlight

Highlight requests default highlighting for the supplied fields.

```go
func (s Search) Highlight(fields ...string) Search
```

### Search.HybridRRF

HybridRRF uses Elasticsearch's RRF retriever, which requires an appropriate
server license. License errors are preserved rather than silently downgraded.

```go
func (s Search) HybridRRF(lexical Query, vector KNN, window, constant int) Search
```

### Search.KNN

KNN sets a validated vector search using the official request model.

```go
func (s Search) KNN(k KNN) Search
```

### Search.MarshalJSON

MarshalJSON serializes the snapshot, returning any deferred construction error.

```go
func (s Search) MarshalJSON() ([]byte, error)
```

### Search.MinScore

MinScore excludes hits whose score is lower than score.

```go
func (s Search) MinScore(score float64) Search
```

### Search.PIT

PIT selects an existing point-in-time ID and renews its keep-alive duration.

```go
func (s Search) PIT(id, keepAlive string) Search
```

### Search.Param

Param returns a copy with an additional search URL parameter. Metadata-hiding
filter_path and partial-result overrides are rejected by the ODM contract.

```go
func (s Search) Param(key, value string) Search
```

### Search.Params

Params returns an independent copy of the URL parameters.

```go
func (s Search) Params() url.Values
```

### Search.Preference

Preference selects the shard or session preference; it cannot be used with a PIT.

```go
func (s Search) Preference(value string) Search
```

### Search.RequestCache

RequestCache enables or disables Elasticsearch's request cache for this search.

```go
func (s Search) RequestCache(enabled bool) Search
```

### Search.Routing

Routing targets shards for the supplied routing values; it cannot be used with a PIT.

```go
func (s Search) Routing(values ...string) Search
```

### Search.Runtime

Runtime sets dynamic runtime field definitions; official runtime types can
also be supplied through SearchFromRequest or With.

```go
func (s Search) Runtime(fields map[string]any) Search
```

### Search.SearchAfter

SearchAfter sets the last hit sort values without converting JSON numbers.

```go
func (s Search) SearchAfter(values ...json.RawMessage) Search
```

### Search.Size

Size sets the maximum hits per page; negative values defer a validation error.

```go
func (s Search) Size(n int) Search
```

### Search.Sort

Sort sets the ordered sort keys, rejecting empty field names.

```go
func (s Search) Sort(fields ...Sort) Search
```

### Search.SortBy

SortBy accepts the complete official sort model, including nested and missing options.

```go
func (s Search) SortBy(sorts ...types.SortCombinations) Search
```

### Search.Source

Source selects source fields to include or exclude. Partial sources must not
be used for a full-document replacement without restoring omitted fields.

```go
func (s Search) Source(includes, excludes []string) Search
```

### Search.Suggest

Suggest sets a named suggester of kind using the supplied field and text.
Deprecated: Use SuggestWith with the official FieldSuggester type.

```go
func (s Search) Suggest(name, field, text, kind string) Search
```

### Search.SuggestWith

SuggestWith sets an official typed suggester without positional option strings.

```go
func (s Search) SuggestWith(name string, suggester types.FieldSuggester) Search
```

### Search.Timeout

Timeout sets the server search timeout, rounded up to whole milliseconds.

```go
func (s Search) Timeout(duration time.Duration) Search
```

### Search.TypedKeys

TypedKeys requests aggregation type prefixes, enabling Aggregates decoding.

```go
func (s Search) TypedKeys(enabled bool) Search
```

### Search.With

With is an immutable escape hatch for additional search body options.

```go
func (s Search) With(key string, value any) Search
```

### SearchResult

SearchResult contains typed documents, metadata and raw aggregation results.
A non-nil search error can accompany a partially populated result.

```go
// SearchResult contains typed documents, metadata and raw aggregation results.
// A non-nil search error can accompany a partially populated result.
type SearchResult[T any] struct {
	// Took is server search processing time in milliseconds.
	Took int64 `json:"took"`
	// TimedOut reports server timeout and makes the result incomplete.
	TimedOut bool `json:"timed_out"`
	// PITID is the newest PIT identifier; it can change between pages. Empty means none was
	// returned.
	PITID string `json:"pit_id,omitempty"`
	// Hits contains this page, its total count and maximum score.
	Hits struct {
		// Total is the server hit count or lower bound.
		Total Total `json:"total"`
		// Hits contains this page of documents in response order.
		Hits []Hit[T] `json:"hits"`
		// MaxScore is nil when scores are not available.
		MaxScore *float64 `json:"max_score"`
	} `json:"hits"`
	// Aggregations contains raw JSON keyed by aggregation name, or type#name when typed_keys is
	// enabled.
	Aggregations map[string]json.RawMessage `json:"aggregations,omitempty"`
	// Suggest contains raw named suggestion responses; nil means none were returned.
	Suggest map[string]json.RawMessage `json:"suggest,omitempty"`
	// Shards reports searched shards and failures. A nonzero failed count yields PartialSearchError.
	Shards struct {
		// Total is the number of shards participating in the search.
		Total int `json:"total"`
		// Failed is the count of unsuccessful shards.
		Failed int `json:"failed"`
		// Failures holds raw shard errors, which can contain sensitive data.
		Failures []json.RawMessage `json:"failures,omitempty"`
	} `json:"_shards"`
}
```

### InnerHits

InnerHits decodes one named inner-hit group as U. Source documents are decoded
without repository hooks or metadata hydration because no U repository is bound.
An absent group returns ErrNotFound; malformed JSON returns a decoding error.

```go
func InnerHits[U any, T any](hit Hit[T], name string) (SearchResult[U], error)
```

### SearchResult[T].Aggregates

Aggregates decodes typed_keys aggregation results using the official response decoder.
Unknown aggregation types and ambiguous normalized names are rejected.

```go
func (r SearchResult[T]) Aggregates() (map[string]types.Aggregate, error)
```

### SearchResult[T].Exact

Exact reports whether Total is an exact count.

```go
func (r SearchResult[T]) Exact() bool
```

### SearchResult[T].Len

Len returns the number of hits in this page.

```go
func (r SearchResult[T]) Len() int
```

### SearchResult[T].Sources

Sources returns source values in hit order. Maps and pointers within T are shared with the hits.

```go
func (r SearchResult[T]) Sources() []T
```

### SearchResult[T].Total

Total returns the server hit count; consult Exact to distinguish lower bounds.

```go
func (r SearchResult[T]) Total() int64
```

### Sort

Sort describes an ascending or descending field sort.

```go
// Sort describes an ascending or descending field sort.
type Sort struct {
	// Field is the required Elasticsearch field name or supported special sort such as _score or
	// _shard_doc.
	Field string
	// Desc selects descending order when true; false selects ascending order.
	Desc bool
}
```

### Stamper

Stamper opts a document into audit timestamps. StampFields returns top-level JSON names.
Implementations must mutate only the receiver's own fields or anonymous embedded models.

```go
// Stamper opts a document into audit timestamps. StampFields returns top-level JSON names.
// Implementations must mutate only the receiver's own fields or anonymous embedded models.
type Stamper interface {
	Stamp(operation Operation, now time.Time)
	StampFields() (created, updated string)
}
```

### TextField

TextField adds analyzed text queries to a string field descriptor.

```go
// TextField adds analyzed text queries to a string field descriptor.
type TextField struct {
	Field[string]
}
```

### Text

Text creates a descriptor for an analyzed text field.

```go
func Text(name string) TextField
```

### TextField.Keyword

Keyword selects the conventional keyword multi-field; it must exist in the mapping.

```go
func (f TextField) Keyword() Field[string]
```

### TextField.Match

Match analyzes text and matches it against the field.

```go
func (f TextField) Match(value string) Query
```

### TextField.Phrase

Phrase matches analyzed terms in phrase order.

```go
func (f TextField) Phrase(value string) Query
```

### Timestamps

Timestamps provides opt-in UTC creation and modification dates. Embed by value or pointer.

```go
// Timestamps provides opt-in UTC creation and modification dates. Embed by value or pointer.
type Timestamps struct {
	// CreatedAt is preserved when nonzero; otherwise Stamp sets it to the current UTC audit time.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is replaced by Stamp on full-document writes. Partial updates add an audit time only
	// when the patch does not already contain updated_at.
	UpdatedAt time.Time `json:"updated_at"`
}
```

### *Timestamps.Stamp

Stamp preserves an existing creation date and updates the modification date.

```go
func (t *Timestamps) Stamp(_ Operation, now time.Time)
```

### *Timestamps.StampFields

StampFields returns the JSON names used for partial updates.

```go
func (*Timestamps) StampFields() (string, string)
```

### Total

Total reports the hit count and whether it is exact (eq) or a lower bound (gte).

```go
// Total reports the hit count and whether it is exact (eq) or a lower bound (gte).
type Total struct {
	// Value is the reported hit count or lower bound; inspect Relation.
	Value int64 `json:"value"`
	// Relation is eq for an exact count or gte for a lower bound; empty means no total was reported.
	Relation string `json:"relation"`
}
```

### Transport

Transport is implemented by the official Elasticsearch client. Authentication,
connection pooling, TLS, compression and transport retries belong to that client.

```go
// Transport is implemented by the official Elasticsearch client. Authentication,
// connection pooling, TLS, compression and transport retries belong to that client.
type Transport interface {
	Perform(*http.Request) (*http.Response, error)
}
```

### TransportFunc

TransportFunc adapts a function to Transport, useful for deterministic tests.

```go
// TransportFunc adapts a function to Transport, useful for deterministic tests.
type TransportFunc func(*http.Request) (*http.Response, error)
```

### TransportFunc.Perform

Perform invokes f with the request.

```go
func (f TransportFunc) Perform(req *http.Request) (*http.Response, error)
```

### UpdateOptions

UpdateOptions adds server-side optimistic conflict retries to ordinary write options.
RetryOnConflict cannot be combined with explicit concurrency tokens.

```go
// UpdateOptions adds server-side optimistic conflict retries to ordinary write options.
// RetryOnConflict cannot be combined with explicit concurrency tokens.
type UpdateOptions struct {
	WriteOptions
	// RetryOnConflict is a nonnegative server-side retry count; zero omits it. Positive values
	// cannot be combined with explicit concurrency tokens.
	RetryOnConflict int
}
```

### UpdateResult

UpdateResult includes optional updated source requested through update.Request.Source_.

```go
// UpdateResult includes optional updated source requested through update.Request.Source_.
type UpdateResult[T any] struct {
	WriteResult
	// Get is the optional updated source response; nil means it was not returned. It is decoded
	// without repository AfterRead hooks.
	Get *Hit[T] `json:"get,omitempty"`
}
```

### Version

Version is an Elasticsearch server semantic version.

```go
// Version is an Elasticsearch server semantic version.
type Version struct {
	// Major, Minor and Patch are nonnegative semantic version components.
	Major, Minor, Patch int
}
```

### ParseVersion

ParseVersion parses three nonnegative version components, ignoring a hyphen suffix.

```go
func ParseVersion(s string) (Version, error)
```

### Version.Compare

Compare orders semantic versions and returns -1, 0 or 1.

```go
func (v Version) Compare(other Version) int
```

### Version.String

String formats the version as major.minor.patch.

```go
func (v Version) String() string
```

### WriteOption

WriteOption configures a document write. Options are applied in order.
WriteOptions replaces all options; subsequent functional options override fields.

```go
// WriteOption configures a document write. Options are applied in order.
// WriteOptions replaces all options; subsequent functional options override fields.
type WriteOption interface {
	// contains filtered or unexported methods
}
```

### IfMatches

IfMatches applies an independent copy of metadata's concurrency tokens and routing.

```go
func IfMatches(metadata Metadata) WriteOption
```

### WithPipeline

WithPipeline selects an ingest pipeline.

```go
func WithPipeline(pipeline string) WriteOption
```

### WithRefresh

WithRefresh controls write visibility.

```go
func WithRefresh(refresh Refresh) WriteOption
```

### WithRouting

WithRouting selects document routing.

```go
func WithRouting(routing string) WriteOption
```

### WriteOptions

WriteOptions configures routing, refresh, concurrency control and ingest.

```go
// WriteOptions configures routing, refresh, concurrency control and ingest.
type WriteOptions struct {
	// Routing must match the value used when the document was indexed.
	Routing string
	// Refresh accepts empty, false, true or wait_for.
	Refresh Refresh
	// IfSeqNo and IfPrimaryTerm must be supplied together for concurrency control.
	IfSeqNo *int64
	// IfPrimaryTerm must accompany IfSeqNo and be at least one; nil omits concurrency control only
	// when both tokens are nil.
	IfPrimaryTerm *int64
	// Pipeline applies only to index/create operations.
	Pipeline string
}
```

### WriteResult

WriteResult describes an acknowledged document operation and shard outcomes.

```go
// WriteResult describes an acknowledged document operation and shard outcomes.
type WriteResult struct {
	Metadata
	// Result is the server outcome, normally created, updated, deleted, not_found or noop. Unknown
	// future values are preserved.
	Result string `json:"result"`
	// Shards reports replication outcomes. A successful HTTP status does not imply every replica
	// succeeded.
	Shards struct {
		// Total is the number of shard copies targeted.
		Total int `json:"total"`
		// Successful is the number of successful shard copies.
		Successful int `json:"successful"`
		// Failed is the number of failed shard copies.
		Failed int `json:"failed"`
	} `json:"_shards"`
}
```

## github.com/ctolon/esodm/adapter/es8

Package es8 connects esodm to the official Elasticsearch 8 client.

### BulkIndexerItem

BulkIndexerItem prepares a document for the official indexer, preserving ODM
validation and completion hooks. onResult is required and may run concurrently.
Configure batch pipeline/refresh on the indexer; per-item values are rejected.
If indexer.Add fails, invoke the returned item's OnFailure with that error.

```go
func BulkIndexerItem[T any](ctx context.Context, repo *esodm.Repository[T], op esodm.BulkOperation[T], onResult func(context.Context, esodm.BulkItem)) (esutil.BulkIndexerItem, error)
```

### Connect

Connect accepts a configured official client. Set DisableRetry: true in its
configuration for writes that cannot safely be replayed after an ambiguous error.

```go
func Connect(ctx context.Context, client *elasticsearch.Client, config esodm.Config) (*esodm.Client, error)
```

## github.com/ctolon/esodm/adapter/es9

Package es9 connects esodm to the official Elasticsearch 9 client.

### BulkIndexerItem

BulkIndexerItem prepares a document for the official indexer, preserving ODM
validation and completion hooks. onResult is required and may run concurrently.
Configure batch pipeline/refresh on the indexer; per-item values are rejected.
If indexer.Add fails, invoke the returned item's OnFailure with that error.

```go
func BulkIndexerItem[T any](ctx context.Context, repo *esodm.Repository[T], op esodm.BulkOperation[T], onResult func(context.Context, esodm.BulkItem)) (esutil.BulkIndexerItem, error)
```

### Connect

Connect binds a configured official client and verifies server major 9.
Configure authentication, retries and pooling on the official client.

```go
func Connect(ctx context.Context, client *elasticsearch.Client, config esodm.Config) (*esodm.Client, error)
```

## github.com/ctolon/esodm/migrationstore

Package migrationstore provides local, atomic storage for migration checkpoints.

### File

File stores one migration checkpoint. Its zero value is not usable; set Path
before use. Do not copy a File after use or change Path concurrently. Calls on
one instance are serialized. Applications must serialize other processes and
instances that share the path, and keep the containing directory trusted.
Atomic replacement and directory syncing require a filesystem that supports
them. A Save error after rename may leave the new checkpoint installed.

```go
// File stores one migration checkpoint. Its zero value is not usable; set Path
// before use. Do not copy a File after use or change Path concurrently. Calls on
// one instance are serialized. Applications must serialize other processes and
// instances that share the path, and keep the containing directory trusted.
// Atomic replacement and directory syncing require a filesystem that supports
// them. A Save error after rename may leave the new checkpoint installed.
type File struct {
	// Path is the checkpoint file path. Its parent directory must already exist;
	// use an application-controlled path and serialize writers.
	Path string
	// contains filtered or unexported fields
}
```

### *File.Load

Load reads at most 4 MiB from Path. A missing file returns an error matching
os.ErrNotExist. ResumeMigration validates the decoded state against the cluster
before acting on it.

```go
func (f *File) Load() (esodm.MigrationState, error)
```

### *File.Save

Save writes JSON to a private temporary file, syncs it, atomically replaces Path,
and syncs its parent directory on Unix. Windows does not guarantee directory-entry durability. The parent directory must already exist.
Save can be assigned directly to MigrationOptions.Checkpoint.

```go
func (f *File) Save(state esodm.MigrationState) (err error)
```

## github.com/ctolon/esodm/observe/otel

Package otel provides an optional OpenTelemetry observer. It records operation
duration and result status without collecting document contents or identifiers.

### Observer

Observer creates client spans using tracer without recording documents or IDs.

```go
func Observer(tracer trace.Tracer) esodm.Observer
```

## github.com/ctolon/esodm/esodmtest

Package esodmtest provides deterministic HTTP fixtures for applications using esodm.

### Expectation

Expectation is a single response fixture. Configure it before sending requests.

```go
// Expectation is a single response fixture. Configure it before sending requests.
type Expectation struct {
	// contains filtered or unexported fields
}
```

### *Expectation.Reply

Reply configures the HTTP status and JSON response body for this expectation.

```go
func (e *Expectation) Reply(status int, body string) *Expectation
```

### Recorder

Recorder matches the first unused matching expectation in registration order.
Different paths need not arrive in registration order. Expectations are single-use. Its zero
value is usable. Configure expectations before issuing concurrent requests.

```go
// Recorder matches the first unused matching expectation in registration order.
// Different paths need not arrive in registration order. Expectations are single-use. Its zero
// value is usable. Configure expectations before issuing concurrent requests.
type Recorder struct {
	// contains filtered or unexported fields
}
```

### NewClient

NewClient creates an ODM client with a declared version and a recording transport.
No network or version-discovery request is made.

```go
func NewClient(t TestingT, version esodm.Version) (*esodm.Client, *Recorder)
```

### *Recorder.AssertAll

AssertAll reports unused expectations and unexpected requests.

```go
func (r *Recorder) AssertAll(t TestingT)
```

### *Recorder.On

On adds an expectation with an anchored regular expression for the escaped path.
Invalid patterns panic, like regexp.MustCompile. Query parameters are available
in Requests for separate assertions and are not part of the path match.

```go
func (r *Recorder) On(method, pathPattern string) *Expectation
```

### *Recorder.Perform

Perform records a request and consumes the first matching unused expectation.
Unexpected requests return an error and are also reported by AssertAll.

```go
func (r *Recorder) Perform(req *http.Request) (*http.Response, error)
```

### *Recorder.Requests

Requests returns independent snapshots in arrival order.

```go
func (r *Recorder) Requests() []Request
```

### Request

Request is an owned snapshot of a recorded HTTP request. Body and headers may
contain application data; do not publish recordings without reviewing them.

```go
// Request is an owned snapshot of a recorded HTTP request. Body and headers may
// contain application data; do not publish recordings without reviewing them.
type Request struct {
	// Method is the recorded HTTP method.
	Method string
	// Path is the escaped URL path without query parameters.
	Path string
	// Query is the raw URL query string, without the leading question mark.
	Query string
	// Header is an independent copy of request headers and may contain credentials.
	Header http.Header
	// Body is an independent copy of the request payload and may contain application data.
	Body []byte
}
```

### TestingT

TestingT is the test reporting interface used by NewClient and AssertAll.

```go
// TestingT is the test reporting interface used by NewClient and AssertAll.
type TestingT interface {
	Helper()
	Fatalf(string, ...any)
	Errorf(string, ...any)
}
```
