# Document operations

## Writes

| Operation | Behavior |
| --- | --- |
| `Create(ctx, id, doc, opts...)` | Creates the document; an existing ID returns `ErrConflict` |
| `Insert(ctx, doc, opts...)` | Creates the document with a server-generated ID |
| `Replace(ctx, id, doc, opts...)` | Indexes the complete document, creating or replacing it |
| `Save(ctx, doc, opts...)` | `Replace` using `DocumentID()` and `DocumentRouting()` from the model |
| `Update(ctx, id, patch, opts...)` | Merges a partial document |
| `Upsert(ctx, id, patch, create, opts...)` | Merges the patch, or creates from `create` when the document is missing |
| `UpdateWith(ctx, id, request, options)` | Runs an official update request, including scripts; see [Scripts and tasks](operations.md) |
| `Delete(ctx, id, opts...)` | Deletes the document |

<!-- source: examples/guide/crud.go -->
```go
package guide

import (
	"context"

	"github.com/ctolon/esodm"
)

// CreateProduct creates a document and waits for search visibility.
func CreateProduct(ctx context.Context, repo *esodm.Repository[Product]) (esodm.WriteResult, error) {
	return repo.Create(ctx, "book-1", Product{Name: "Go book", Category: "books", Price: 25},
		esodm.WithRefresh(esodm.RefreshWaitFor))
}

// ChangePrice uses both concurrency tokens from the last read.
func ChangePrice(ctx context.Context, repo *esodm.Repository[Product], id, routing string, price int) (esodm.WriteResult, error) {
	hit, err := repo.GetWith(ctx, id, esodm.ReadOptions{Routing: routing})
	if err != nil {
		return esodm.WriteResult{}, err
	}
	patch, err := esodm.NewPatch(esodm.OrderedValue[int]("price").Set(price))
	if err != nil {
		return esodm.WriteResult{}, err
	}
	return repo.Update(ctx, id, patch, esodm.IfMatches(hit.Metadata))
}

// SaveProduct replaces a document using model ID and routing.
func SaveProduct(ctx context.Context, repo *esodm.Repository[Product]) (esodm.WriteResult, error) {
	return repo.Save(ctx, Product{
		DocumentMeta: esodm.DocumentMeta{ID: "book-2", Routing: "tenant-1"},
		Name:         "Search systems", Price: 40,
	})
}
```

Write options are functional: `WithRefresh(RefreshWaitFor)`, `WithRouting("tenant-1")`, `WithPipeline("enrich")` and `IfMatches(hit.Metadata)`. A `WriteOptions` struct is also an option and replaces the whole set; later options override individual fields. `Refresh` accepts `RefreshFalse`, `RefreshTrue` and `RefreshWaitFor`; the zero value uses the server default. `Pipeline` applies to `Create`, `Insert`, `Replace` and `Save`.

`Insert` rejects a model whose `DocumentID()` is non-empty. Every write returns a `WriteResult` with ID, index, routing, sequence number, primary term, version, result and shard counts. The caller's struct is not modified; read the document back when the stored form is needed.

## Reads

| Operation | Behavior |
| --- | --- |
| `GetByID(ctx, id)` | Loads an unrouted document; a missing document returns `ErrNotFound` |
| `GetWith(ctx, id, ReadOptions{...})` | Loads with routing, preference, realtime and source filter options |
| `ExistsWith(ctx, id, ReadOptions{...})` | HEAD request; returns `false` for a missing document |
| `MGetWith(ctx, ids, ReadOptions{...})` | Loads several documents in input order and reports missing ones per item |
| `Count(ctx, query)`, `CountWith(ctx, query, CountOptions{...})` | Counts matches |

`Get`, `Exists` and `MGet` with a routing string argument remain available. Routing must match the value used when the document was written. Reads on a join index require explicit routing; see [Relationships](relationships.md).

## Patches

A `Patch` is a map of the fields to change. Absent keys are untouched, zero values are written and `nil` writes JSON null. Nested objects are patched with nested maps, not dotted keys. `NewPatch` builds a patch from typed assignments:

```go
patch, err := esodm.NewPatch(ProductFields.Price.Set(30), ProductFields.Name.Clear())
if err != nil {
	return err
}
```

`Clear()` writes JSON null. In this example a later read into a string Name produces its zero value; use a pointer field when null must remain distinguishable. Application hooks such as ProductHooks may reject clearing a required name.

`BeforePatch` receives a copy of the patch and may add or change fields. `Timestamps` adds `updated_at` when the patch does not set it.

## Optimistic concurrency

Every hit and write result carries `SeqNo` and `PrimaryTerm`. `IfMatches(hit.Metadata)` (or `hit.Conditional()` as a `WriteOptions` value) makes the next write conditional on both values and reuses the routing. A mismatch returns `ErrConflict`; reload the document and decide how to merge. `Create` and `Insert` cannot carry concurrency tokens.

Do not replace a document from a source-filtered read; fields omitted by the filter would be removed. On Elasticsearch 9, full-document reads of a schema with vector fields request the vectors explicitly so a read-and-replace cycle keeps them.

## Failure semantics

- A successful write followed by a failing `AfterWrite` hook returns `CommittedError`; `Result` holds the write result and `Unwrap` returns the hook error.
- A transport error can occur after the server has applied a write. Neither case means the write is safe to repeat; use `Create` or conditional writes when idempotency matters.
- Validation errors from hooks are wrapped in `ErrValidation` and keep the original error for `errors.As`.
