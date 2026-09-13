# Options and behavioral contracts

This guide covers esodm-owned configuration. The [API reference](api.md) documents every public field; the topic guides demonstrate complete workflows. Values passed through official request types are validated by the connected Elasticsearch version. There is no implied support for an option simply because it exists in the v9 Go types.

## Common conventions

- All operation contexts must be non-nil. Use `context.Background()` for a root and derive deadlines for network work. A server-side timeout is separate from the caller context deadline.
- Constructors return usable values only when the error is nil. A zero `Client`, `Repository`, `Schema` or `Iterator` is not usable. A zero `Registry` is usable; do not copy it after use.
- Query, Search, Aggregation and Finder values are immutable snapshots. Their builder methods return a new value; retain the return value. Construction errors are deferred to `Err`, serialization or execution as supported by each type.
- A zero Query matches all documents. A zero Search is an empty search body. Use explicit queries for destructive by-query operations.
- Nil pointer options mean “omit.” Pointers to false or zero explicitly transmit those values when the option supports them. Empty strings and slices generally omit an optional parameter; the field reference identifies exceptions.
- “Server default” means esodm omits a property; index settings and server versions may affect the effective behavior. esodm does not freeze those defaults.
- Input documents are passed by value, with anonymous embedded struct pointers initialized and copied during write preparation. Named pointers, maps and slices remain shared; hooks must not mutate shared nested data. Patches are shallow copies. Clone nested values in application code before modifying them.
- Repositories and clients are reusable across goroutines. Hooks, custom clocks and observers must be concurrency-safe. Iterators and mutable official request builders must not be used concurrently.
- Results can accompany errors. Check the error before using a result; inspect per-item errors for multi-get, multi-search and bulk operations. No multi-document API is a transaction.

## Connection configuration

| Field | Accepted values and default | Behavior |
| --- | --- | --- |
| `Config.Now` | `func() time.Time`; nil selects `time.Now` | Audit timestamps are normalized to UTC. Called during preparation, not only after successful writes. |
| `Config.MaxResponseBytes` | 1..1,099,511,627,776 bytes; zero selects 33,554,432 bytes (32 MiB) | Bounds the raw buffered response with one extra byte used to detect overflow. Decoded objects and concurrent requests allocate additional memory. Applies to success and error responses, including discarded bodies. |
| `Config.Observer` | `Observer`; nil disables | Synchronous request completion callback. It runs before the operation returns and adds to caller latency. Errors can contain sensitive data. |

`Connect` discovers the version with `GET /`; `ConnectVersion` trusts the version supplied by the caller. Both apply the same configuration validation and support limits. Use the adapter corresponding to the server major. Authentication, certificates, compression, HTTP timeouts and whole-request retries belong to the official client configuration.

## Option application order

`NewSchema` accepts `SchemaOption` values in order. `WithDynamic`, `WithSettings` and `WithProperties` replace their respective field. A `SchemaConfig` replaces the entire configuration; it does not merge with preceding options. Empty `Dynamic` resolves to `strict`. Settings and properties are serialized into snapshots when construction succeeds.

`NewRepository` accepts `RepositoryOption[T]` values in order. Both `Hooks[T]` and `WithHooks` replace the entire hook set. Nil callbacks disable the corresponding hook; multiple hook sets are not composed.

Write operations accept `WriteOption` values in order. `WriteOptions` replaces the entire option set; individual `With...` options override their field. Nil options are validation errors. After options are applied, empty write routing can be populated by a model implementing `Routed`. An empty explicit routing string therefore does not suppress model routing.

## Reads and writes

| Option | Accepted values | Default and restrictions |
| --- | --- | --- |
| `WriteOptions.Refresh` / `WithRefresh` | empty, `RefreshFalse` (`false`), `RefreshTrue` (`true`), `RefreshWaitFor` (`wait_for`) | Empty omits refresh. True requests immediate refresh; wait_for waits for visibility and can extend request duration. |
| `WriteOptions.Routing` / `WithRouting` | route string | Empty permits model fallback on full writes. Reads, updates and deletes must use the route used for indexing. Join repositories require explicit routing for reads and partial operations. |
| `WriteOptions.IfSeqNo` | nil or integer ≥ 0 | Must be paired with `IfPrimaryTerm`. Zero is a valid token and must be passed by pointer. |
| `WriteOptions.IfPrimaryTerm` | nil or integer ≥ 1 | Both nil omit OCC. Create and Insert reject OCC. Use `Metadata.Conditional()` after a read returning both tokens. |
| `WriteOptions.Pipeline` / `WithPipeline` | pipeline name | Empty omits it. Only full index/create operations support it; update and delete reject it. The server validates pipeline existence. |
| `ReadOptions.Realtime` | nil, pointer to false, pointer to true | Nil leaves server GET behavior unchanged. Search remains near-real-time regardless of this setting. |
| `ReadOptions.Source` | nil or `types.SourceFilter` | Includes/Excludes accept field patterns; empty lists omit filtering. Excludes remove matching included fields. Filtered models may be incomplete and must not be blindly saved as replacements. |
| `ReadOptions.Preference` / `CountOptions.Preference` | preference string | Empty omits it. Custom stable session strings and server preference expressions are passed through. |
| `CountOptions.Routing` | slice of route strings | Empty omits routing; values are comma-joined. |
| `UpdateOptions.RetryOnConflict` | integer ≥ 0 | Zero omits it. Positive values request server-side conflict retries and cannot accompany explicit OCC tokens. This is separate from bulk retry and transport retry. |

Explicit document IDs must be valid UTF-8 and 1..512 **bytes**, not characters. IDs containing slashes, `.` or `..` are escaped as path segments. `Insert` returns a generated ID and never writes it back into the caller model. `Save` requires `Identified`; it is full replacement, not a patch. `Patch` distinguishes an absent key, a zero value and explicit null (`nil`). Arrays in patches are replacement values, not append instructions.

## Mapping values and tags

`Dynamic` accepts exactly `strict`, `true`, `false` and `runtime`; the inferred schema default is `strict`. `FieldMapping.Type` is an open Elasticsearch mapping type name, not a closed Go enum. esodm validates structural limits while Elasticsearch validates type-specific combinations and availability.

The complete tag grammar is comma-separated `key=value`, with no escaping layer. Duplicate keys, unknown keys, missing `=` and empty values are errors. Empty segments are ignored. Whitespace is not automatically trimmed. A value containing a comma cannot be represented safely in a tag; use `FieldMapping` or `SchemaFromMapping` instead.

| Tag | Values |
| --- | --- |
| `type` | Nonempty mapping type name; overrides inference. |
| `analyzer`, `search_analyzer` | Nonempty built-in or configured analyzer name. Definitions belong in index settings. |
| `format` | Nonempty Elasticsearch date format, optionally using `||` alternatives; not a Go time layout. |
| `index`, `doc_values` | Exactly `true` or `false`. |
| `ignore_above` | Nonnegative decimal integer; server validates applicability to the field type. |
| `null_value` | JSON scalar: string, number, boolean or null. Not an object or array; server validates whether the selected field type supports it. |
| `copy_to` | Nonempty field names separated by `|`; empty destinations are errors. |
| `dims` | Nonnegative decimal integer; inferred dense_vector mappings require 1..4096. |
| `similarity` | Nonempty server-supported similarity name. Vector and text similarities have different semantics. |
| `fields` | Exactly `keyword`, creating a named keyword multi-field. Use `FieldMapping.Fields` for arbitrary multi-fields. |
| `dynamic` | Exactly `strict`, `true`, `false` or `runtime`. |
| `inference_id` | Nonempty inference endpoint name for semantic_text. |

`es:"-"` is only allowed on fields excluded from JSON. A mapping override must name a field in the inferred model and preserve every explicit tag setting on that field. Overrides replace a property's mapping rather than deep-merge it. Recursive inferred models, ambiguous promoted fields and mapping depth over 32 are rejected. `SchemaFromMapping` bypasses inference and snapshots official mapping/settings values; use it for custom JSON representations and unsupported inference cases.

## Bulk scheduling and retries

| Field | Values and default |
| --- | --- |
| `BulkStreamOptions.BatchSize` | 1..10000 operations; zero → 500. Each batch also has a 16 MiB encoded NDJSON limit. |
| `Workers` | 1..64; zero → 1. Multiple workers can finish out of order. |
| `QueueSize` | 1..64 batches; zero → 2. Unused by synchronous sequence consumption. |
| `FlushInterval` | Nonnegative `time.Duration`; zero disables timed flushing. A positive interval selects streaming for sequence producers. |
| `Refresh` | Same values as write refresh, applied to each batch. Per-item refresh is rejected. |
| `Retry.MaxRetries` | 0..20 additional attempts; zero disables retries. |
| `Retry.InitialBackoff` | Nonnegative duration; zero → 100ms. |
| `Retry.MaxBackoff` | Nonnegative duration; zero → 30s. When both values are explicit, initial must not exceed maximum. |

Retry uses exponential delay capped at MaxBackoff, randomized in its upper half, and interrupted by context cancellation. Only explicit item statuses 429, 502, 503 and 504 are eligible. Successful items and completion-hook failures are never replayed. The prepared payload is reused, so pre-write hooks and timestamps are not regenerated for retries. Whole-request failures have potentially unknown outcomes and are not retried by esodm; official transport retries remain independently configurable.

`BulkSeq`/`BulkSeq2` run synchronously unless Workers > 1 or FlushInterval > 0. Producers must honor `yield(false)` and must not block indefinitely. `BulkStream` never closes the caller's channel. The caller must cancel or stop its producer when the consumer returns; internal cancellation cannot cancel a context owned by the caller. Callbacks are serialized but batches can arrive out of order. A callback error stops delivery and cancels pending work; already issued writes may still commit.

For the external esutil indexer, configure refresh and pipeline on the indexer. `BulkIndexerItem` rejects per-item values. Call the returned item's `OnFailure` if `Add` fails. Prepared completion callbacks must be invoked exactly once, including failures. Close the indexer before process exit and inspect item failures as well as the Close error.

## Pagination, relationships and migrations

| API or option | Contract |
| --- | --- |
| Iterator page size | 1..10000; no zero default. |
| PIT keep-alive | Empty → `1m`; otherwise a positive duration accepted by Go `time.ParseDuration`. This wrapper does not accept Elasticsearch-only units such as `d`. |
| Cursor | Required PIT ID and nonempty sort values; encoded JSON ≤ 16384 bytes. URL-safe unpadded base64; not signed, encrypted or an authorization token. |
| Finder.Page | One-based positive page and positive size; overflow rejected. Server max_result_window still applies. |
| `PreloadOptions.MaxDepth` | 1..32; zero → 3; roots count as the first level. Reaching the depth limit sets Truncated when unvisited references remain. |
| `PreloadOptions.MaxDocuments` | Positive; zero → 10000. Counts distinct references including missing ones. Exceeding it returns the partial graph and ErrValidation. |
| `MigrationOptions.WritesPaused` | Must be true; caller also serializes migrations and alias administration. No automatic lock or writer pause. |
| `MigrationOptions.PollInterval` | Zero → 1s for asynchronous resume; positive durations control task polling. Negative values are rejected before side effects. Synchronous ApplyMigration does not poll. |
| `MigrationOptions.Checkpoint` | Nil disables persistence; otherwise it must durably save before returning nil. Receives an independent checkpoint copy. |
| `migrationstore.File.Path` | Application-controlled path in an existing trusted directory. Checkpoints are limited to 4 MiB; serialize separate instances/processes yourself. |

Iterators own their PIT until Close or Detach. Close is idempotent and uses an independent five-second timeout. Range-over-function helpers close on early exit but cannot yield a cleanup error after the consumer has stopped; use an explicit Iterator and deferred Close when cleanup errors must be observed. Resumption requires the same query and sort and transfers PIT ownership to the resumed iterator.

See [migration recovery](migrations.md) for checkpoint phases and reconciliation. A failed migration retains both indexes; a timeout does not roll back writes or cancel a server task automatically.

The [example catalog](examples.md) maps each behavior family to application code and regression coverage. [Executable contract examples](../example_contracts_test.go) demonstrate option precedence, nil versus false, OCC conflicts, committed hook errors, bulk retry and missing references.
