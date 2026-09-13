package esodm

import (
	"context"
	"errors"
	"fmt"
	"iter"
)

// BulkSeq consumes a sequence in bounded batches. Multiple workers or a flush
// interval select BulkStream scheduling; otherwise consumption is synchronous.
// Producers must stop when yield returns false and honor ctx during blocking work.
// Result callbacks are serial; parallel batches may complete out of input order.
func (r *Repository[T]) BulkSeq(ctx context.Context, seq iter.Seq[BulkOperation[T]], options BulkStreamOptions, onResult func(BulkBatchResult) error) error {
	if seq == nil {
		return fmt.Errorf("%w: nil bulk sequence", ErrValidation)
	}
	return r.BulkSeq2(ctx, func(yield func(BulkOperation[T], error) bool) {
		if seq == nil {
			yield(BulkOperation[T]{}, fmt.Errorf("%w: nil sequence", ErrValidation))
			return
		}
		seq(func(op BulkOperation[T]) bool { return yield(op, nil) })
	}, options, onResult)
}

// BulkSeq2 accepts a fallible producer, including a transformation of Repository.Each.
// Accepted buffered operations are flushed before returning a producer error.
func (r *Repository[T]) BulkSeq2(ctx context.Context, seq iter.Seq2[BulkOperation[T], error], options BulkStreamOptions, onResult func(BulkBatchResult) error) error {
	if seq == nil || onResult == nil || options.Workers > 64 || options.Workers < 0 || options.FlushInterval < 0 || options.QueueSize < 0 || options.QueueSize > 64 || options.BatchSize < 0 || options.BatchSize > 10000 {
		return fmt.Errorf("%w: invalid bulk sequence options", ErrValidation)
	}
	if err := options.Retry.validate(); err != nil {
		return err
	}
	if _, err := (WriteOptions{Refresh: options.Refresh}).values(); err != nil {
		return err
	}
	if options.Workers > 1 || options.FlushInterval > 0 {
		return r.streamSequence(ctx, seq, options, onResult)
	}
	size := options.BatchSize
	if size == 0 {
		size = 500
	}
	batch := make([]BulkOperation[T], 0, size)
	sequence := 0
	var first error
	flush := func() bool {
		if len(batch) == 0 {
			return true
		}
		result, err := r.BulkWithOptions(ctx, batch, BulkOptions{Refresh: options.Refresh, Retry: options.Retry})
		batch = make([]BulkOperation[T], 0, size)
		if first == nil {
			first = err
		}
		callbackErr := onResult(BulkBatchResult{Sequence: sequence, Result: result, Err: err})
		sequence++
		if callbackErr != nil {
			first = callbackErr
			return false
		}
		return true
	}
	for op, err := range seq {
		if e := ctx.Err(); e != nil {
			return e
		}
		if err != nil {
			if flush() {
				first = errors.Join(first, err)
			}
			return first
		}
		batch = append(batch, op)
		if len(batch) == size && !flush() {
			return first
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	flush()
	return first
}

// streamSequence joins its producer before returning, including on stream errors.
// The unbuffered bridge leaves batching and backpressure with BulkStream.
func (r *Repository[T]) streamSequence(ctx context.Context, seq iter.Seq2[BulkOperation[T], error], options BulkStreamOptions, onResult func(BulkBatchResult) error) error {
	producerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	input := make(chan BulkOperation[T])
	done := make(chan error, 1)
	go func() {
		var producerErr error
		defer func() { close(input); done <- producerErr }()
		for op, err := range seq {
			if err != nil {
				producerErr = err
				return
			}
			if producerCtx.Err() != nil {
				return
			}
			select {
			case input <- op:
			case <-producerCtx.Done():
				return
			}
		}
	}()
	streamErr := r.BulkStream(producerCtx, input, options, onResult)
	cancel()
	return errors.Join(streamErr, <-done)
}

// Each opens and owns a PIT for the duration of a range-over-function loop.
func (r *Repository[T]) Each(ctx context.Context, search Search, pageSize int, keepAlive string) iter.Seq2[Hit[T], error] {
	return func(yield func(Hit[T], error) bool) {
		it, err := r.Iterate(ctx, search, pageSize, keepAlive)
		if err != nil {
			yield(Hit[T]{}, err)
			return
		}
		it.Each()(yield)
	}
}

// Sources yields only source documents and iteration errors, closing its PIT on exit.
func (r *Repository[T]) Sources(ctx context.Context, search Search, pageSize int, keepAlive string) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		r.Each(ctx, search, pageSize, keepAlive)(func(hit Hit[T], err error) bool { return yield(hit.Source, err) })
	}
}
