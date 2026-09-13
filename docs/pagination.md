# Pagination

`Size` and `From` page through the first `index.max_result_window` hits (10,000 by default). Beyond that, or when a scan must be stable while documents change, use a point in time (PIT) with `search_after`.

## Iterators

`Repository.Iterate(ctx, search, pageSize, keepAlive)` opens a PIT, adds a `_shard_doc` tiebreaker when the search has no sort, and returns an `Iterator` that owns the PIT.

<!-- source: examples/guide/iteration.go -->
```go
package guide

import (
	"context"
	"errors"

	"github.com/ctolon/esodm"
)

// VisitProducts owns its PIT and preserves cleanup errors after early returns.
func VisitProducts(ctx context.Context, repo *esodm.Repository[Product], visit func(Product) error) (err error) {
	it, err := repo.Iterate(ctx, esodm.NewSearch(esodm.MatchAll()), 100, "1m")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, it.Close()) }()
	for it.Next() {
		if err := visit(it.Hit().Source); err != nil {
			return err
		}
	}
	return it.Err()
}
```

`Next` fetches pages as needed, `Hit` returns the current hit and `Err` reports the traversal or cleanup error. `Close` releases the PIT with its own five-second timeout, independent of the request context, and can be called more than once. The iterator closes itself on exhaustion, error or context cancellation. It is not safe for concurrent use.

`Iterator.Each()` and `Repository.Each(ctx, search, pageSize, keepAlive)` expose the same traversal as `iter.Seq2[Hit[T], error]`; leaving the loop early closes the PIT. `Repository.Sources` yields documents only. `Finder.Each` uses a one-minute keep-alive.

A search used for iteration must not set `from`, `search_after`, `pit`, `collapse` or a retriever. Routing and preference are applied when the PIT is opened and removed from the page requests.

## Cursors

After a successful `Next`, `Cursor()` returns the PIT ID and the sort values of the current hit. `Detach()` returns the same cursor, stops the iterator and hands ownership of the PIT to the caller without closing it. `EncodeCursor` and `DecodeCursor` convert a cursor to and from a URL-safe string.

`Repository.ResumeIterator(ctx, search, cursor, pageSize, keepAlive)` continues from a cursor. Pass the same query and sort as the original search, without routing or preference. The resumed iterator owns the PIT again; do not use the original iterator afterwards.

A PIT expires after its keep-alive. Cursors are not signed and carry no authorization: validate a cursor received from a client against the caller's access rights, and sign or encrypt it if it crosses a trust boundary.
