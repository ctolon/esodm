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
