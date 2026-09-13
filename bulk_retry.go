package esodm

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"time"
)

// RetryPolicy enables bounded, jittered retries of explicit 429/502/503/504 item failures.
// Zero MaxRetries disables retries. Whole-request/ambiguous transport failures are never retried by the ODM.
type RetryPolicy struct {
	// MaxRetries is the number of additional attempts, from 0 to 20. Zero disables ODM item retries.
	MaxRetries int
	// InitialBackoff is a nonnegative initial delay; zero selects 100ms. When both delays are
	// explicit it must not exceed MaxBackoff.
	InitialBackoff time.Duration
	// MaxBackoff is a nonnegative delay cap; zero selects 30s. Each retry waits in the upper half of
	// the exponentially increasing capped delay, with jitter.
	MaxBackoff time.Duration
}

func (p RetryPolicy) validate() error {
	if p.MaxRetries < 0 || p.MaxRetries > 20 || p.InitialBackoff < 0 || p.MaxBackoff < 0 || p.InitialBackoff > 0 && p.MaxBackoff > 0 && p.InitialBackoff > p.MaxBackoff {
		return fmt.Errorf("%w: invalid retry policy", ErrValidation)
	}
	return nil
}
func (p RetryPolicy) delay(attempt int) time.Duration {
	initial, maximum := p.InitialBackoff, p.MaxBackoff
	if initial == 0 {
		initial = 100 * time.Millisecond
	}
	if maximum == 0 {
		maximum = 30 * time.Second
	}
	d := min(initial, maximum)
	for range attempt {
		if d > maximum/2 {
			d = maximum
			break
		}
		d *= 2
	}
	if d < 2 {
		return d
	}
	return d/2 + time.Duration(rand.Int64N(int64(d-d/2)))
}

// BulkOptions controls refresh and opt-in item retry.
type BulkOptions struct {
	// Refresh accepts empty, false, true or wait_for and applies to each submitted batch, including
	// retries. Empty preserves the server default.
	Refresh Refresh
	// Retry enables opt-in retries of explicit 429/502/503/504 item failures; its zero value
	// disables retries. Transport failures are never retried by the ODM.
	Retry RetryPolicy
}

func normalizeBulk[T any](op BulkOperation[T]) (BulkOperation[T], error) {
	if op.Action == BulkIndex || op.Action == BulkCreate {
		op.Options = modelOptions(&op.Document, op.Options)
		if op.ID == "" {
			if identified, ok := any(&op.Document).(Identified); ok {
				initializeEmbeds(reflect.ValueOf(&op.Document).Elem(), 0)
				op.ID = identified.DocumentID()
				if op.ID == "" && op.Action != BulkCreate {
					return op, fmt.Errorf("%w: empty model document ID", ErrValidation)
				}
			}
		}
	}
	return op, nil
}

// BulkWithOptions prepares each document once, then retries only explicit transient
// item failures. Successful items and AfterWrite failures are never replayed.
// Output order always matches input. Context/transport failures return known results plus an error.
func (r *Repository[T]) BulkWithOptions(ctx context.Context, ops []BulkOperation[T], options BulkOptions) (BulkResult, error) {
	var out BulkResult
	if err := options.Retry.validate(); err != nil {
		return out, err
	}
	if _, err := (WriteOptions{Refresh: options.Refresh}).values(); err != nil {
		return out, err
	}
	if len(ops) == 0 {
		return out, nil
	}
	if len(ops) > 10000 {
		return out, fmt.Errorf("%w: bulk exceeds 10000 operations", ErrValidation)
	}
	prepared := make([]BulkOperation[T], len(ops))
	lines := make([][]byte, len(ops))
	pending := make([]int, len(ops))
	totalBytes := 0
	for i, op := range ops {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		var err error
		prepared[i], err = normalizeBulk(op)
		if err != nil {
			return out, err
		}
		header, body, err := r.EncodeOperation(ctx, prepared[i])
		if err != nil {
			return out, err
		}
		line := append(header, '\n')
		if len(body) > 0 {
			line = append(line, body...)
			line = append(line, '\n')
		}
		lines[i] = line
		totalBytes += len(line)
		if totalBytes > 16<<20 {
			return out, fmt.Errorf("%w: bulk exceeds 16 MiB", ErrValidation)
		}
		pending[i] = i
	}
	out.Items = make([]BulkItem, len(ops))
	for i, op := range prepared {
		out.Items[i] = BulkItem{Action: op.Action, Err: errors.New("esodm: write outcome unknown")}
	}
	for attempt := 0; ; attempt++ {
		var body []byte
		batch := make([]BulkOperation[T], len(pending))
		for j, i := range pending {
			body = append(body, lines[i]...)
			batch[j] = prepared[i]
		}
		result, err := r.sendBulk(ctx, batch, body, options.Refresh)
		out.Took += result.Took
		var itemError *BulkError
		if err != nil && !errors.As(err, &itemError) {
			// A malformed later item must not erase earlier acknowledged writes.
			for j, item := range result.Items {
				out.Items[pending[j]] = item
			}
			return out, err
		}
		var retry []int
		for j, item := range result.Items {
			i := pending[j]
			out.Items[i] = item
			var e *Error
			var committed *CommittedError
			if attempt < options.Retry.MaxRetries && !errors.As(item.Err, &committed) && errors.As(item.Err, &e) && e.Retryable() {
				retry = append(retry, i)
			}
		}
		if len(retry) == 0 {
			break
		}
		if err = waitContext(ctx, options.Retry.delay(attempt)); err != nil {
			return out, err
		}
		pending = retry
	}
	failed := 0
	for _, item := range out.Items {
		if item.Err != nil {
			failed++
		}
	}
	if failed > 0 {
		return out, &BulkError{Failed: failed}
	}
	return out, nil
}
