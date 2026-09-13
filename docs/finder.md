# Finder

`Repository.Find` starts an immutable query chain bound to the repository. Every method returns a new value, so a chain can be extended conditionally and reused concurrently.

<!-- source: examples/guide/finder.go -->
```go
package guide

import (
	"context"

	"github.com/ctolon/esodm"
)

// FindAffordableBooks returns one search window while retaining hit metadata.
func FindAffordableBooks(ctx context.Context, products *esodm.Repository[Product]) ([]esodm.Hit[Product], error) {
	price := esodm.OrderedValue[int]("price")
	return products.Find(esodm.Match("name", "Go")).
		Filter(esodm.Term("category", "books"), price.LTE(30)).
		Sort(price.Asc()).Size(20).All(ctx)
}
```

```go
query := products.Find(esodm.Match("name", "Go"))
if category != "" {
	query = query.Filter(esodm.Term("category", category))
}
hits, err := query.Size(20).All(ctx)
if err != nil {
	return err
}
```

## Composition

| Method | Effect |
| --- | --- |
| `Find(q...)` | Required scoring clauses; no argument matches everything |
| `Must(q...)` | Adds required scoring clauses |
| `Filter(q...)` | Adds required clauses that do not affect scores |
| `Should(q...)` | Adds optional scoring clauses; at least one must match when there are no required clauses |
| `Not(q...)` | Excludes matches |
| `FindSearch(search)` | Starts from an existing `Search`, keeping its query and options |

`Size`, `From`, `Sort`, `Source`, `Highlight`, `Collapse`, `KNN`, `Routing`, `Preference` and `With` set the same options as on `Search`. `Aggregate(name, aggregation)` adds a named aggregation and replaces one with the same name. `Search()` returns the compiled snapshot for use with `MultiSearch` or `Iterate`; `Err()` reports construction errors without a request.

## Terminal operations

| Method | Result |
| --- | --- |
| `Result(ctx)` | The full `SearchResult[T]` |
| `All(ctx)` | Hits from one search window |
| `Sources(ctx)` | Documents from one search window |
| `First(ctx)` | One hit at the configured offset; `ErrNotFound` when nothing matches |
| `Count(ctx)` | Match count through `_count`; ignores paging, sorting, aggregations and KNN |
| `Exists(ctx)` | Whether at least one document matches; requests a single hit and stops early where the server allows |
| `Page(ctx, number, size)` | A one-based window with `Total`, `Exact` and `HasNext` |
| `Each(ctx, pageSize)` | Iterates every match through a PIT with a one-minute keep-alive |
| `EachSource(ctx, pageSize)` | The same iteration yielding documents |

Partial results are returned together with `PartialSearchError`; check the error even when hits are present. `Page.HasNext` is derived from the reported total, so it is conservative when the total is a lower bound. Windows beyond `index.max_result_window` need `Each`.

`Each` closes the PIT when the loop ends, breaks or fails. A cleanup error can only be reported while the consumer is still receiving values; use `Repository.Iterate` when that error must be inspected after an early stop, or when a different keep-alive is needed. A chain that already carries `from`, `search_after`, `pit`, `collapse` or a retriever cannot be iterated.

A chain with only a `KNN` clause and no lexical query runs as a pure vector search; no `match_all` is added.
