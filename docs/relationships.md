# Relationships

## Embedded objects

A struct field is stored inside its parent document as an object. Map it as `nested` when queries must match fields of the same array element together; `Nested(path, query)` and generated `ObjectField.Nested` build such queries.

## References

`Ref[T]` stores the index, ID and optional routing of another document. `Load[T](ctx, client, refs)` fetches references with `_mget`, deduplicating identical references and requesting at most 500 per call, and returns one `ReferenceResult` per input in input order. Check `Found` and `Err` per result. `Relation[T](name)` is the descriptor form with a `Load` method; on Go 1.27, `client.Load[T]` is equivalent.

`Repository.MGet` loads IDs from the repository's own index and runs `AfterRead`. Package-level `Load` runs no hooks.

<!-- source: examples/guide/relations.go -->
```go
package guide

import (
	"context"

	"github.com/ctolon/esodm"
)

// RelatedProducts follows same-type references with explicit graph limits.
func RelatedProducts(ctx context.Context, client *esodm.Client, roots []esodm.Ref[Product]) (esodm.Graph[Product], error) {
	return esodm.Preload(ctx, client, roots, func(p Product) []esodm.Ref[Product] {
		return p.Related
	}, esodm.PreloadOptions{MaxDepth: 3, MaxDocuments: 1000})
}
```

`Preload` follows references breadth-first from the given roots, visiting each document once and stopping at `MaxDepth` (reported by `Graph.Truncated`) or failing beyond `MaxDocuments`. It returns the loaded documents as a graph and does not modify the source models. Heterogeneous references are loaded with separate typed calls.

## Parent-child joins

A join index has one `join` field declared with `FieldMapping{Type: "join", Relations: map[string]any{"question": "answer"}}`. Documents store an `esodm.Join` value: a parent stores its relation name, a child stores its name and the parent ID. Child writes require routing, and every read, update and delete on a join index requires explicit routing, including for parents, so that a family stays on one shard. Use the root ancestor's routing at every generation.

`HasChild`, `HasParent` and `ParentID` build the corresponding queries. The complete write and query flow is exercised in [the integration suite](../integration/integration_test.go).

Elasticsearch has no cross-document transactions or foreign keys. esodm does not cascade deletes, load references implicitly or track loaded documents; those policies belong to the application.
