package esodm

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestBulkSeqTimerFlushAndProducerError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	flushed := make(chan struct{})
	repo := testRepo(t, func(*http.Request) (*http.Response, error) {
		return response(200, `{"items":[{"create":{"_id":"x","status":201}}]}`), nil
	})
	producerErr := errors.New("producer failed")
	seq := func(yield func(BulkOperation[testDoc], error) bool) {
		if !yield(BulkOperation[testDoc]{Action: BulkCreate}, nil) {
			return
		}
		select {
		case <-flushed:
			yield(BulkOperation[testDoc]{}, producerErr)
		case <-ctx.Done():
			yield(BulkOperation[testDoc]{}, ctx.Err())
		}
	}
	count := 0
	err := repo.BulkSeq2(ctx, seq, BulkStreamOptions{BatchSize: 10, FlushInterval: time.Millisecond}, func(b BulkBatchResult) error {
		count++
		close(flushed)
		return b.Err
	})
	if !errors.Is(err, producerErr) || count != 1 {
		t.Fatal(count, err)
	}
}

func TestBulkSeqParallelAndCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var requests atomic.Int32
	repo := testRepo(t, func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		started <- struct{}{}
		select {
		case <-release:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		return response(200, `{"items":[{"create":{"_id":"x","status":201}}]}`), nil
	})
	go func() {
		for range 2 {
			select {
			case <-started:
			case <-ctx.Done():
				return
			}
		}
		close(release)
	}()
	seq := func(yield func(BulkOperation[testDoc]) bool) {
		for range 2 {
			if !yield(BulkOperation[testDoc]{Action: BulkCreate}) {
				return
			}
		}
	}
	sequences := map[int]bool{}
	err := repo.BulkSeq(ctx, seq, BulkStreamOptions{Workers: 2, BatchSize: 1}, func(b BulkBatchResult) error {
		sequences[b.Sequence] = true
		return b.Err
	})
	if err != nil || requests.Load() != 2 || !sequences[0] || !sequences[1] {
		t.Fatal(err, requests.Load(), sequences)
	}

	repo = testRepo(t, func(*http.Request) (*http.Response, error) {
		return response(200, `{"items":[{"create":{"_id":"x","status":201}}]}`), nil
	})
	stopped := false
	infinite := func(yield func(BulkOperation[testDoc]) bool) {
		defer func() { stopped = true }()
		for {
			if !yield(BulkOperation[testDoc]{Action: BulkCreate}) {
				return
			}
		}
	}
	callbackErr := errors.New("stop")
	callbacks := 0
	err = repo.BulkSeq(ctx, infinite, BulkStreamOptions{Workers: 2, BatchSize: 1}, func(BulkBatchResult) error {
		callbacks++
		return callbackErr
	})
	if !errors.Is(err, callbackErr) || !stopped || callbacks != 1 {
		t.Fatal(err, stopped, callbacks)
	}
}
