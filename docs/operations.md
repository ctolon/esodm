# Scripts and tasks

<!-- source: examples/guide/operations.go -->
```go
package guide

import (
	"context"
	"encoding/json"

	"github.com/ctolon/esodm"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/deletebyquery"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/update"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/updatebyquery"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

// IncrementPrice runs a server-side script with a bounded conflict retry count.
func IncrementPrice(ctx context.Context, repo *esodm.Repository[Product], id string) (esodm.UpdateResult[Product], error) {
	return repo.UpdateWith(ctx, id, &update.Request{
		Script:  &types.Script{Source: "ctx._source.price += params.step", Params: map[string]json.RawMessage{"step": json.RawMessage("1")}},
		Source_: true,
	}, esodm.UpdateOptions{RetryOnConflict: 3})
}

// RepriceBooks submits an asynchronous operation and returns its task response.
func RepriceBooks(ctx context.Context, repo *esodm.Repository[Product]) (updatebyquery.Response, error) {
	return repo.UpdateByQuery(ctx, &updatebyquery.Request{
		Query:  &types.Query{Term: map[string]types.TermQuery{"category": {Value: "books"}}},
		Script: &types.Script{Source: "ctx._source.price = params.price", Params: map[string]json.RawMessage{"price": json.RawMessage("30")}},
	}, esodm.ByQueryOptions{Async: true})
}

// DeleteCategory explicitly scopes deletion by category.
func DeleteCategory(ctx context.Context, repo *esodm.Repository[Product], category string) (deletebyquery.Response, error) {
	return repo.DeleteByQuery(ctx, &deletebyquery.Request{
		Query: &types.Query{Term: map[string]types.TermQuery{"category": {Value: category}}},
	}, esodm.ByQueryOptions{Refresh: true})
}
```

## Script updates

`UpdateWith(ctx, id, request, options)` runs an official `update.Request`. A `Doc` patch goes through stamping and `BeforePatch`; an upsert document is decoded as the model, stamped and validated. A `Script` runs on the server, so Go hooks cannot see the resulting document; include audit fields in the script when they matter. `AfterWrite` runs after a successful update in both cases.

`UpdateOptions` embeds `WriteOptions` and adds `RetryOnConflict`, the server-side retry count for version conflicts. It cannot be combined with `IfSeqNo` and `IfPrimaryTerm`. `Doc` and `Script` are mutually exclusive. Pass script parameters through `Params`; never interpolate untrusted input into the script source.

## Update and delete by query

`UpdateByQuery` and `DeleteByQuery` accept the official request types and require an explicit query; use `MatchAll` deliberately for a whole index. `ByQueryOptions` sets routing, refresh, the conflicts policy and asynchronous execution. Documents are processed on the server, so per-document hooks and timestamps do not run.

A synchronous call returns the official response with its counters. Failures, timeouts, or version conflicts under the default `abort` policy return `IncompleteOperationError` alongside the response. Documents already changed by an incomplete operation are not rolled back.

## Tasks

With `Async: true`, the response carries a task ID. `Admin().Task(ctx, id)` reads its state, `WaitTask(ctx, id, interval)` polls until completion and `CancelTask(ctx, id)` requests cancellation. Cancelling the context of `WaitTask` stops polling but not the server task. Persist task IDs when the initiating process may exit before the task completes; task results are kept by the server for a limited time.
