package esodm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

// BulkAction identifies an Elasticsearch bulk operation kind.
type BulkAction string

const (
	// BulkIndex creates or replaces a complete document.
	BulkIndex BulkAction = "index"
	// BulkCreate creates a document only when its ID is absent.
	BulkCreate BulkAction = "create"
	// BulkUpdate merges a partial document patch.
	BulkUpdate BulkAction = "update"
	// BulkDelete removes a document.
	BulkDelete BulkAction = "delete"
)

// BulkOperation carries a document or patch and per-item write options.
// Refresh must be set on the batch rather than on individual operations.
type BulkOperation[T any] struct {
	// Action must be BulkIndex, BulkCreate, BulkUpdate or BulkDelete; the zero value is invalid.
	Action BulkAction
	// ID identifies the target; empty permits model ID inference for full documents and server-
	// generated IDs for BulkCreate only.
	ID string
	// Document is used by index/create; other actions ignore it. Preparation runs stamping and full-
	// document hooks.
	Document T
	// Patch is used by update; other actions ignore it. Missing fields are untouched and nil values
	// write JSON null.
	Patch Patch
	// Options supplies per-item routing, pipeline and concurrency tokens. Refresh is rejected here;
	// set it on the batch.
	Options WriteOptions
}

// BulkItem reports one operation outcome, including hook or server errors.
type BulkItem struct {
	// Action identifies the corresponding input operation.
	Action BulkAction
	WriteResult
	// Status is the per-item HTTP status; zero means no definitive server outcome was decoded.
	Status int
	// Err is the item failure or CommittedError; nil means success. Inspect alongside Status and
	// WriteResult.
	Err error
}

// BulkResult preserves request order and server processing time in milliseconds.
type BulkResult struct {
	// Took is accumulated server processing time in milliseconds across attempts; it excludes client
	// backoff.
	Took int64
	// Items preserves input order. On request failure, prepared items can have unknown outcomes;
	// inspect the returned error before trusting results.
	Items []BulkItem
}

// BulkError reports failed items; inspect BulkResult.Items for individual causes.
type BulkError struct {
	// Failed is the count of final failed items; inspect BulkResult.Items for their causes.
	Failed int
}

// Error returns a human-readable description of the failure.
func (e *BulkError) Error() string {
	return fmt.Sprintf("esodm: %d bulk operations failed; inspect Items", e.Failed)
}

func (r *Repository[T]) prepareBulk(ctx context.Context, op BulkOperation[T]) (types.OperationContainer, []byte, error) {
	if err := validateIndexName(r.Index()); err != nil {
		return types.OperationContainer{}, nil, err
	}
	var header types.OperationContainer
	var normalizeErr error
	op, normalizeErr = normalizeBulk(op)
	if normalizeErr != nil {
		return header, nil, normalizeErr
	}
	if err := validateID(op.ID); err != nil && !(op.Action == BulkCreate && op.ID == "") {
		return header, nil, err
	}
	if _, err := op.Options.values(); err != nil {
		return header, nil, err
	}
	if op.Options.Refresh != "" {
		return header, nil, fmt.Errorf("%w: set refresh on the bulk request", ErrValidation)
	}
	if op.Action == BulkCreate && op.Options.IfSeqNo != nil {
		return header, nil, fmt.Errorf("%w: create cannot use concurrency tokens", ErrValidation)
	}
	var pipeline, routing *string
	var documentID *string
	if op.ID != "" {
		documentID = &op.ID
	}
	if op.Options.Routing != "" {
		routing = &op.Options.Routing
	}
	if op.Options.Pipeline != "" {
		if op.Action != BulkIndex && op.Action != BulkCreate {
			return header, nil, fmt.Errorf("%w: pipeline only applies to index/create", ErrValidation)
		}
		pipeline = &op.Options.Pipeline
	}
	var body []byte
	var err error
	switch op.Action {
	case BulkIndex, BulkCreate:
		r.stamp(&op.Document, op.Action)
		if r.hooks.BeforeWrite != nil {
			if err := r.hooks.BeforeWrite(ctx, op.Action, &op.Document); err != nil {
				return header, nil, err
			}
		}
		if r.hooks.Validate != nil {
			if err := r.hooks.Validate(ctx, &op.Document); err != nil {
				return header, nil, fmt.Errorf("%w: %w", ErrValidation, err)
			}
		}
		body, err = json.Marshal(op.Document)
		if err == nil {
			err = r.schema.validateJoin(body, op.Options.Routing)
		}
		if op.Action == BulkIndex {
			header.Index = &types.IndexOperation{Index_: &r.schema.index, Id_: documentID, Routing: routing, IfSeqNo: op.Options.IfSeqNo, IfPrimaryTerm: op.Options.IfPrimaryTerm, Pipeline: pipeline}
		} else {
			header.Create = &types.CreateOperation{Index_: &r.schema.index, Id_: documentID, Routing: routing, Pipeline: pipeline}
		}
	case BulkUpdate:
		if err = r.schema.requireJoinRouting(op.Options.Routing); err != nil {
			return header, nil, err
		}
		patch, hookErr := r.preparePatch(ctx, op.ID, op.Patch)
		if hookErr != nil {
			return header, nil, hookErr
		}
		body, err = json.Marshal(struct {
			Doc Patch `json:"doc"`
		}{patch})
		header.Update = &types.UpdateOperation{Index_: &r.schema.index, Id_: documentID, Routing: routing, IfSeqNo: op.Options.IfSeqNo, IfPrimaryTerm: op.Options.IfPrimaryTerm}
	case BulkDelete:
		err = r.schema.requireJoinRouting(op.Options.Routing)
		if err == nil && r.hooks.BeforeDelete != nil {
			err = r.hooks.BeforeDelete(ctx, op.ID)
		}
		header.Delete = &types.DeleteOperation{Index_: &r.schema.index, Id_: documentID, Routing: routing, IfSeqNo: op.Options.IfSeqNo, IfPrimaryTerm: op.Options.IfPrimaryTerm}
	default:
		return header, nil, fmt.Errorf("%w: invalid bulk action", ErrValidation)
	}
	if err != nil {
		return header, nil, fmt.Errorf("%w: %w", ErrValidation, err)
	}
	return header, body, nil
}

// EncodeOperation runs pre-write hooks and validation and encodes one operation
// for an external bulk indexer. Returned lines exclude trailing newlines.
// It does not send a request or invoke AfterWrite; the external indexer owns results.
func (r *Repository[T]) EncodeOperation(ctx context.Context, op BulkOperation[T]) (metadata, body []byte, err error) {
	header, body, err := r.prepareBulk(ctx, op)
	if err != nil {
		return nil, nil, err
	}
	metadata, err = json.Marshal(header)
	return metadata, body, err
}

// Bulk submits at most 10,000 operations and 16 MiB of encoded NDJSON.
// Failed items are reported individually and are not retried by the ODM.
func (r *Repository[T]) Bulk(ctx context.Context, ops []BulkOperation[T], refresh Refresh) (BulkResult, error) {
	return r.BulkWithOptions(ctx, ops, BulkOptions{Refresh: refresh})
}
func (r *Repository[T]) sendBulk(ctx context.Context, ops []BulkOperation[T], body []byte, refresh Refresh) (BulkResult, error) {
	var out BulkResult

	q := url.Values{}
	if refresh != "" {
		q.Set("refresh", string(refresh))
	}
	var response struct {
		Took  int64 `json:"took"`
		Items []map[string]struct {
			WriteResult
			Status int               `json:"status"`
			Error  *types.ErrorCause `json:"error"`
		} `json:"items"`
	}
	if err := r.client.request(ctx, "POST", "/_bulk", q, body, "application/x-ndjson", &response); err != nil {
		return out, err
	}
	if len(response.Items) != len(ops) {
		return out, fmt.Errorf("esodm: bulk response count mismatch (write outcome may be partial)")
	}
	out.Took = response.Took
	out.Items = make([]BulkItem, 0, len(ops))
	failed := 0
	for i, item := range response.Items {
		entry, ok := item[string(ops[i].Action)]
		if !ok || len(item) != 1 {
			return out, fmt.Errorf("esodm: bulk response action mismatch")
		}
		result := BulkItem{Action: ops[i].Action, WriteResult: entry.WriteResult, Status: entry.Status}
		result.Routing = ops[i].Options.Routing
		if entry.Error != nil {
			result.Err = &Error{Status: entry.Status, Type: entry.Error.Type, Reason: errorReason(entry.Error), Cause: *entry.Error}
		} else if entry.Status < 200 || entry.Status >= 300 {
			result.Err = &Error{Status: entry.Status}
		} else {
			result.Err = r.afterWrite(ctx, ops[i].Action, result.WriteResult, nil)
		}
		if result.Err != nil {
			failed++
		}
		out.Items = append(out.Items, result)
	}
	if failed > 0 {
		return out, &BulkError{failed}
	}
	return out, nil
}

// BulkStreamOptions bounds batch size, worker count and queued batches.
// Zero values select 500 items, one worker and two queued batches.
type BulkStreamOptions struct {
	// FlushInterval flushes partial batches periodically; zero disables the timer.
	FlushInterval time.Duration
	// BatchSize is the maximum operations per batch, 1..10000; zero selects 500. Each encoded batch
	// must also fit within 16 MiB.
	BatchSize int
	// Workers is the concurrent batch worker count, 1..64; zero selects one. More than one worker
	// allows out-of-order completion.
	Workers int
	// QueueSize is the number of queued batches, 1..64; zero selects two. It has no effect on
	// synchronous BulkSeq execution.
	QueueSize int
	// Refresh accepts empty, false, true or wait_for for each batch; empty preserves the server
	// default.
	Refresh Refresh
	// Retry applies only to item failures explicitly returned by Elasticsearch.
	Retry RetryPolicy
}

// BulkBatchResult associates an asynchronously completed batch with its input sequence.
type BulkBatchResult struct {
	// Sequence is the zero-based batch number assigned in producer order; delivery order may differ.
	Sequence int
	// Result contains known item outcomes for this batch.
	Result BulkResult
	// Err is the batch-level error, including BulkError for item failures; nil means the batch
	// succeeded.
	Err error
}

// BulkStream consumes until input closes, flushing the last batch. The bounded
// batch queue provides backpressure. Results may arrive out of order and include
// their sequence number. onResult is invoked serially. Returning an error cancels
// pending work; callers must stop their producer when ctx is canceled. Failed
// items are delivered; Retry explicitly enables transient item retries. A non-nil batch error is also
// returned after all accepted batches finish.
func (r *Repository[T]) BulkStream(ctx context.Context, input <-chan BulkOperation[T], options BulkStreamOptions, onResult func(BulkBatchResult) error) error {
	if options.BatchSize == 0 {
		options.BatchSize = 500
	}
	if options.Workers == 0 {
		options.Workers = 1
	}
	if options.QueueSize == 0 {
		options.QueueSize = 2
	}
	if options.FlushInterval < 0 || input == nil || onResult == nil ||
		options.BatchSize < 1 || options.BatchSize > 10000 ||
		options.Workers < 1 || options.Workers > 64 ||
		options.QueueSize < 1 || options.QueueSize > 64 {
		return fmt.Errorf("%w: invalid bulk stream options", ErrValidation)
	}
	if err := options.Retry.validate(); err != nil {
		return err
	}
	if _, err := (WriteOptions{Refresh: options.Refresh}).values(); err != nil {
		return err
	}
	parent := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type batch struct {
		seq int
		ops []BulkOperation[T]
	}
	jobs := make(chan batch, options.QueueSize)
	results := make(chan BulkBatchResult, options.Workers)
	var wg sync.WaitGroup
	wg.Add(options.Workers + 1)
	go func() {
		defer wg.Done()
		defer close(jobs)
		var ticks <-chan time.Time
		if options.FlushInterval > 0 {
			ticker := time.NewTicker(options.FlushInterval)
			defer ticker.Stop()
			ticks = ticker.C
		}
		seq := 0
		ops := make([]BulkOperation[T], 0, options.BatchSize)
		send := func() bool {
			if len(ops) == 0 {
				return true
			}
			select {
			case jobs <- batch{seq, ops}:
				seq++
				ops = make([]BulkOperation[T], 0, options.BatchSize)
				return true
			case <-ctx.Done():
				return false
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticks:
				if !send() {
					return
				}
			case op, ok := <-input:
				if !ok {
					send()
					return
				}
				ops = append(ops, op)
				if len(ops) == options.BatchSize && !send() {
					return
				}
			}
		}
	}()
	for range options.Workers {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-jobs:
					if !ok {
						return
					}
					result, err := r.BulkWithOptions(ctx, job.ops, BulkOptions{Refresh: options.Refresh, Retry: options.Retry})
					select {
					case results <- BulkBatchResult{job.seq, result, err}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	go func() { wg.Wait(); close(results) }()
	var first error
	callbackFailed := false
	for result := range results {
		if first == nil && result.Err != nil {
			first = result.Err
		}
		if !callbackFailed {
			if err := onResult(result); err != nil {
				first = err
				callbackFailed = true
				cancel()
			}
		}
	}
	if parent.Err() != nil {
		return parent.Err()
	}
	return first
}
