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
