# Queries and search

`Query`, `Aggregation` and `Search` are immutable values. Constructors serialize their input immediately, so a value can be built once and reused across goroutines. Official `esdsl` builders are mutable until they are snapshotted with `FromQuery` or `FromAggregation`.

<!-- source: examples/guide/search.go -->
```go
package guide

import (
	"context"
	"time"

	"github.com/ctolon/esodm"
	"github.com/elastic/go-elasticsearch/v9/typedapi/esdsl"
)

// FindBooks returns a sorted page with highlighting and aggregation results.
func FindBooks(ctx context.Context, repo *esodm.Repository[Product]) (esodm.SearchResult[Product], error) {
	query := esodm.And(
		esodm.FromQuery(esdsl.NewMatchQuery("name", "Go")),
		esodm.Filter(esodm.Term("category", "books"), esodm.OrderedValue[int]("price").LTE(30)),
	)
	search := esodm.NewSearch(query).Size(20).
		Sort(esodm.OrderedValue[int]("price").Asc()).
		Highlight("name").Timeout(2 * time.Second).
		Aggregations(map[string]esodm.Aggregation{
			"average_price": esodm.FromAggregation(esdsl.NewAverageAggregation().Field("price")),
		})
	return repo.Search(ctx, search)
}
```

`NewSearch(query)` enables exact hit totals and sequence number metadata. `SearchFromRequest` snapshots an official `search.Request` and adds those defaults only when absent. Construction errors are deferred: check `Err()` on a query or search, or they surface when the search runs.

## Query helpers

| Purpose | Helpers |
| --- | --- |
| Everything or nothing | `MatchAll`, `MatchNone` |
| Exact values | `Term`, `Terms`, `IDs`, `Field.Eq`, `Field.In` |
| Full text | `Match`, `MatchPhrase`, `MultiMatch`, `TextField.Match`, `TextField.Phrase` |
| Patterns and presence | `Prefix`, `Wildcard`, `Exists`, `Field.Exists` |
| Boolean composition | `And`, `Filter`, `Or`, `Not` |
| Ranges | `GT`, `GTE`, `LT`, `LTE`, `Between` on `OrderedField` and `DateField` |
| Nested and joins | `Nested`, `HasChild`, `HasParent`, `ParentID` |
| Anything else | `FromQuery` with an `esdsl` builder or `types.Query`; `RawQuery` for JSON |

An empty `And` or `Filter` matches everything; an empty `Or` or `IDs` matches nothing. `Filter` clauses do not affect scores. Typed descriptors (`OrderedValue[int]("price")`, `Text("name")`, `Date("created_at")`, or generated `ProductFields`) keep field names and value types together.

## Search options

| Area | Methods |
| --- | --- |
| Paging and order | `Size`, `From`, `Sort`, `SortBy`, descriptor `Asc` and `Desc` |
| Projection | `Source(includes, excludes)` |
| Presentation | `Highlight`, `Collapse`, `SuggestWith` |
| Computation | `Aggregations`, `Runtime`, `MinScore` |
| Request parameters | `Routing`, `Preference`, `RequestCache`, `Timeout`, `TypedKeys`, `Param` |
| Deep pagination | `SearchAfter`, `PIT`; see [Pagination](pagination.md) |
| Vectors | `KNN`, `HybridRRF`; see [Advanced search](advanced-search.md) |
| Escape hatches | `With(key, value)` for body fields, `Param(key, value)` for URL parameters |

A search that carries a PIT cannot set routing or preference; those are fixed when the PIT is opened.

## Results

`SearchResult[T]` holds the hits, the total with its relation, aggregations, suggestions and shard statistics.

- `Sources()` returns the documents, `Len()` the page size, `Total()` the reported total and `Exact()` whether the total is exact.
- Each `Hit[T]` carries the source, ID, index, routing, score, sort values, highlights, inner hits and stored fields.
- `Aggregations` and `Suggest` are raw JSON by name. `Aggregates()` decodes them into official aggregate types when the search used `TypedKeys(true)`; `DecodeAgg[T]` decodes one aggregation into an application type.
- A timeout or failed shard returns `PartialSearchError` together with the hits that were received.

`MultiSearch(ctx, searches...)` runs several searches in one request and returns one result and error per search, in order.

## Defaults, replacement and validation

| Call | Accepted input and behavior |
| --- | --- |
| `Size(n)` | Nonnegative count; zero requests no hits and is useful for aggregations. Omitting Size leaves the server page-size default. |
| `From(n)` | Nonnegative zero-based offset. The server enforces `from + size` against the index result window. |
| `Sort(...)` | Ordered nonempty field names; each `Sort.Desc` defaults to false (ascending). Replaces previous sort keys; an empty call transmits an empty list. |
| `SortBy(...)` | Complete official sort alternatives, including nested, missing and special sorts. Replaces previous sort keys. Server validates combinations. |
| `Source(includes, excludes)` | Field-name patterns; nil/empty lists impose no list restriction. Replaces the source filter. Use `With("_source", false)` to omit source entirely. |
| `Highlight(fields...)` | Requests default highlight settings for these fields and replaces the previous highlight request. Use official types for fragment size, tags and other options. |
| `Collapse(field)` | A collapse field name; server validates that the field and query support collapsing. Iterator helpers reject collapsed searches. |
| `Aggregations(map)` | Replaces the named aggregation map. To add one aggregation while retaining others, use Finder.Aggregate. |
| `Runtime(map)` | Replaces runtime field definitions with a JSON snapshot. Runtime type/script options are server-defined. |
| `SuggestWith(name, suggester)` | Replaces the suggestion map with one named official suggester. For multiple suggesters use SearchFromRequest or With. |
| `Timeout(duration)` | Positive Go duration; rounded up to whole milliseconds. This is a server search timeout, not a context deadline. A timed-out result is incomplete. |
| `MinScore(score)` | Finite numeric score threshold; JSON rejects NaN/infinities. The server validates score semantics. |
| `Routing(values...)` | Comma-joined route values. Empty values transmit an empty parameter; omit the call when routing is unnecessary. |
| `Preference(value)` | Server preference expression or stable custom session value. Cannot accompany a PIT page request. |
| `RequestCache(enabled)` | Explicit boolean; omitting the method leaves caching to server/index policy. |
| `TypedKeys(enabled)` | Explicit boolean; use true before decoding with Aggregates. False keeps ordinary aggregation names. |
| `SearchAfter(values...)` | Raw JSON sort values in exactly the same order and types as the search sort. No conversion through float64 is needed. |
| `PIT(id, keepAlive)` | Existing PIT ID and Elasticsearch keep-alive value. Unlike Iterate, this low-level setter delegates validation to the server. It does not own or close the PIT. |
| `With(key, value)` | Nonempty body key and any JSON-serializable value. Replaces that key; nil sends JSON null rather than removing it. |
| `Param(key, value)` | Nonempty URL parameter key; `filter_path` and `allow_partial_search_results` are reserved and rejected. Unknown keys pass through to the server. |

Convenience setters do not promise complete client-side validation of Elasticsearch expressions. An invalid field, analyzer, script, sort combination or version-specific option may return an HTTP 400 error after reaching the server. Check `Err()` to detect local construction errors before sending.

`MultiSearch` preserves input order, including per-search failures; an empty input returns an empty result without a request. All searches must agree on `typed_keys`. Routing, preference, expand_wildcards, request_cache, ignore_unavailable and allow_no_indices have per-search header equivalents; unsupported URL parameters return ErrUnsupported. A PIT search omits the index header. Always inspect both the outer error and each result's Err.

## Typed fields and query boundaries

`Ordered` includes signed and unsigned integer types, float32, float64, string and their named variants; it excludes bool, uintptr and complex values. `DateField` uses time.Time JSON encoding. `Between(lo, hi)` is inclusive and preserves the supplied bounds; it does not reorder reversed bounds. Strings compare according to their Elasticsearch field mapping, not Go application collation.

`Terms`, `Field.In`, `Or` and `IDs` with no values match nothing. `And`, `Filter` and `Not` with no clauses match everything. A zero Query also matches everything; a zero Aggregation is invalid. `Term`/`Field.Eq` perform exact-value matching and do not analyze text. `TextField.Keyword()` only constructs the `.keyword` field name; that multi-field must exist in the mapping.

`Nested` and `HasChild` disable child score propagation; `HasParent` disables parent scoring. Use an official query builder to select another scoring policy. `RawQuery` requires valid JSON with exactly one root clause but delegates clause validity to Elasticsearch. Empty field names are rejected by field-based wrappers; descriptors do not verify a live mapping.
