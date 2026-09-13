# Bulk processing

## Batches

`Bulk(ctx, ops, refresh)` sends a slice of `BulkOperation[T]` in one request and returns a `BulkItem` per operation in input order. Actions are `BulkIndex`, `BulkCreate`, `BulkUpdate` and `BulkDelete`. A batch holds at most 10,000 operations and 16 MiB of encoded NDJSON. If any item fails, the call returns `BulkError`; inspect `Items` for the individual causes.

Index and create operations run stamping, `BeforeWrite` and `Validate`; updates run `BeforePatch`; deletes run `BeforeDelete`. `AfterWrite` runs per successful item. Index and create operations without an ID take it from `DocumentID()`; a create without any ID receives a server-generated ID. Refresh is set on the batch, not on items.

## Item retries

`BulkWithOptions(ctx, ops, BulkOptions{Retry: RetryPolicy{...}})` retries items that failed with 429, 502, 503 or 504, using exponential backoff with jitter between attempts. `MaxRetries` is at most 20; the default delays are 100 ms initial and 30 s maximum. Items are prepared once and resent with the same bytes. Conflicts, validation failures, committed writes and whole-request transport errors are not retried.

## Streams

<!-- source: examples/guide/bulk.go -->
```go
package guide

import (
	"context"
	"iter"
	"time"

	"github.com/ctolon/esodm"
)

// ImportProducts sends bounded batches and stops consumption on a batch failure.
func ImportProducts(ctx context.Context, repo *esodm.Repository[Product], source iter.Seq2[esodm.BulkOperation[Product], error]) error {
	return repo.BulkSeq2(ctx, source, esodm.BulkStreamOptions{
		BatchSize: 500, Workers: 4, QueueSize: 2, FlushInterval: time.Second,
		Retry: esodm.RetryPolicy{MaxRetries: 3, InitialBackoff: 100 * time.Millisecond, MaxBackoff: 2 * time.Second},
	}, func(batch esodm.BulkBatchResult) error {
		return batch.Err
	})
}
```

`BulkSeq` consumes an `iter.Seq`, `BulkSeq2` a fallible `iter.Seq2` and `BulkStream` a channel. Operations are collected into batches of `BatchSize` (default 500) and flushed when a batch is full, when `FlushInterval` elapses or when the input ends. `Workers` (default 1, at most 64) send batches concurrently; `QueueSize` (default 2, at most 64) bounds the batches waiting for a worker and provides backpressure.

`onResult` is called once per batch, serially, with the batch sequence number, its `BulkResult` and error. Batches sent by parallel workers can complete out of order. Returning an error from the callback cancels the remaining work. The first batch error is also returned by the call after all accepted batches finish.

With one worker and no flush interval, `BulkSeq` and `BulkSeq2` run synchronously on the calling goroutine. A producer error in `BulkSeq2` flushes the operations already accepted before it is returned. Producers must stop when `yield` returns false and must honor the context in their own blocking work.

`EncodeOperation` prepares the metadata and body lines of one operation, running its hooks, for use with an external indexer.

## Official esutil indexer

`adapter/es8.BulkIndexerItem` and `adapter/es9.BulkIndexerItem` convert a `BulkOperation` into an `esutil.BulkIndexerItem` while keeping validation, timestamps, metadata and `AfterWrite`. Use them when byte-based flushing or the official indexer's statistics are wanted.

<!-- source: examples/guide/indexer.go -->
```go
package guide

import (
	"context"
	"errors"
	"sync"

	"github.com/ctolon/esodm"
	"github.com/ctolon/esodm/adapter/es8"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esutil"
)

// IndexWithEsutil uses the official byte-based scheduler and retains ODM callbacks.
func IndexWithEsutil(ctx context.Context, raw *elasticsearch.Client, repo *esodm.Repository[Product], ops []esodm.BulkOperation[Product]) error {
	indexer, err := esutil.NewBulkIndexer(esutil.BulkIndexerConfig{Client: raw, NumWorkers: 2, FlushBytes: 5 << 20})
	if err != nil {
		return err
	}
	var mu sync.Mutex
	var failures []error
	onResult := func(_ context.Context, item esodm.BulkItem) {
		if item.Err != nil {
			mu.Lock()
			failures = append(failures, item.Err)
			mu.Unlock()
		}
	}
	for _, op := range ops {
		item, prepareErr := es8.BulkIndexerItem(ctx, repo, op, onResult)
		if prepareErr != nil {
			err = prepareErr
			break
		}
		if addErr := indexer.Add(ctx, item); addErr != nil {
			item.OnFailure(ctx, item, esutil.BulkIndexerResponseItem{}, addErr)
			err = addErr
			break
		}
	}
	closeErr := indexer.Close(ctx)
	mu.Lock()
	defer mu.Unlock()
	return errors.Join(err, closeErr, errors.Join(failures...))
}
```

The `onResult` callback receives the final `BulkItem` and may be called concurrently. If `Add` rejects an item, call the item's `OnFailure` so that the callback still runs. Pipeline and refresh are configured on the indexer; per-item values are rejected. `PrepareIndexerOperation` exposes the same prepared payload for other indexer integrations.
