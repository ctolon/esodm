//go:build integration

package integration_test

import (
	"context"
	"errors"
	"flag"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ctolon/esodm"
)

var soakDuration = flag.Duration("es-soak", 0, "opt-in endurance duration, e.g. 2h")

func TestSoak(t *testing.T) {
	if *soakDuration <= 0 {
		t.Skip("opt-in endurance test: -es-soak=2h")
	}
	c := client(t)
	index := name()
	cleanupIndex(t, c, index)
	r := repo(t, c, index)
	ctx, cancel := context.WithTimeout(context.Background(), *soakDuration)
	defer cancel()
	baseline := openSearchContexts(t, c)
	var wg sync.WaitGroup
	wg.Go(func() {
		for ctx.Err() == nil {
			for _, err := range r.Find().Each(ctx, 100) {
				if err != nil && ctx.Err() == nil {
					t.Error(err)
					cancel()
				}
				break
			}
		}
	})
	wg.Go(func() {
		for ctx.Err() == nil {
			seq := func(yield func(esodm.BulkOperation[product], error) bool) {
				for i := range 64 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(time.Millisecond):
					}
					if !yield(esodm.BulkOperation[product]{Action: esodm.BulkIndex, ID: "bulk-" + strconv.Itoa(i), Document: product{Title: "bulk soak", Price: i, Vector: []float32{1, 1, 1}}}, nil) {
						return
					}
				}
			}
			err := r.BulkSeq2(ctx, seq, esodm.BulkStreamOptions{Workers: 4, BatchSize: 32, QueueSize: 2, FlushInterval: 5 * time.Millisecond}, func(batch esodm.BulkBatchResult) error { return batch.Err })
			if err != nil && ctx.Err() == nil {
				t.Error(err)
				cancel()
				return
			}
		}
	})
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			id := strconv.Itoa(worker)
			doc := product{Title: "soak", Price: worker, Vector: []float32{1, 1, 1}}
			for ctx.Err() == nil {
				_, err := r.Replace(ctx, id, doc, esodm.WriteOptions{})
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					t.Error(err)
					cancel()
					return
				}
				hit, err := r.Get(ctx, id, "")
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					t.Error(err)
					cancel()
					return
				}
				if hit.Source.Price != worker || len(hit.Source.Vector) != 3 {
					t.Error("source mismatch")
					cancel()
					return
				}
				_, err = r.Update(ctx, id, esodm.Patch{"price": worker}, esodm.WriteOptions{IfSeqNo: hit.SeqNo, IfPrimaryTerm: hit.PrimaryTerm})
				if err != nil && ctx.Err() == nil {
					t.Error(err)
					cancel()
					return
				}
				short, stop := context.WithTimeout(ctx, time.Nanosecond)
				_, err = r.Search(short, esodm.NewSearch(esodm.MatchAll()))
				stop()
				if err == nil {
					t.Error("expired request succeeded")
					cancel()
					return
				}
				if !errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
					t.Error(err)
					cancel()
					return
				}
			}
		}(worker)
	}
	wg.Wait()
	if after := drainContexts(t, c, baseline); after > baseline {
		t.Fatalf("PIT contexts leaked: before=%d after=%d", baseline, after)
	}
}

// drainContexts waits out the one-minute PIT keep-alive so in-flight contexts
// opened just before cancellation can expire. Persistent leaks still fail.
func drainContexts(t *testing.T, c *esodm.Client, baseline int64) int64 {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for {
		after := openSearchContexts(t, c)
		if after <= baseline || time.Now().After(deadline) {
			return after
		}
		time.Sleep(5 * time.Second)
	}
}

// Run endurance checks on a dedicated cluster: other clients' PITs affect this count.
func openSearchContexts(t *testing.T, c *esodm.Client) int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var result struct {
		Nodes map[string]struct {
			Indices struct {
				Search struct {
					OpenContexts int64 `json:"open_contexts"`
				} `json:"search"`
			} `json:"indices"`
		} `json:"nodes"`
	}
	if err := c.Do(ctx, "GET", "/_nodes/stats/indices/search", nil, nil, &result); err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, node := range result.Nodes {
		total += node.Indices.Search.OpenContexts
	}
	return total
}
