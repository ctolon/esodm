//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/ctolon/esodm"
)

func TestFinderAndMappingVerification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := client(t)
	index := name()
	cleanupIndex(t, c, index)
	type document struct {
		Name   string `json:"name"`
		Number int    `json:"number"`
	}
	schema, err := esodm.NewSchema[document](index)
	if err != nil {
		t.Fatal(err)
	}
	r, err := esodm.NewRepository(c, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.EnsureIndex(ctx); err != nil {
		t.Fatal(err)
	}
	if drift, err := r.VerifyIndex(ctx); err != nil || !drift.Matches() {
		t.Fatal(drift, err)
	}
	seq := func(yield func(esodm.BulkOperation[document]) bool) {
		for i := range 1500 {
			if !yield(esodm.BulkOperation[document]{Action: esodm.BulkCreate, Document: document{Name: "Go", Number: i}}) {
				return
			}
		}
	}
	if err := r.BulkSeq(ctx, seq, esodm.BulkStreamOptions{Workers: 2, BatchSize: 500, Refresh: esodm.RefreshWaitFor}, func(batch esodm.BulkBatchResult) error { return batch.Err }); err != nil {
		t.Fatal(err)
	}
	query := r.Find(esodm.Term("name", "Go")).Sort(esodm.OrderedValue[int]("number").Asc())
	page, err := query.Page(ctx, 3, 20)
	if err != nil || page.Total != 1500 || page.Hits[0].Source.Number != 40 || !page.Exact || !page.HasNext {
		t.Fatal(page, err)
	}
	count := 0
	for _, err := range query.Each(ctx, 100) {
		if err != nil {
			t.Fatal(err)
		}
		count++
	}
	if count != 1500 {
		t.Fatal(count)
	}
	if ok, err := query.Filter(esodm.Term("number", 1499)).Exists(ctx); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if ok, err := query.Filter(esodm.Term("number", -1)).Exists(ctx); err != nil || ok {
		t.Fatal(ok, err)
	}
	if err := c.Admin().PutMapping(ctx, index, map[string]any{"properties": map[string]any{"new_field": map[string]any{"type": "keyword"}}}); err != nil {
		t.Fatal(err)
	}
	drift, err := r.VerifyIndex(ctx)
	if err != nil || len(drift.ExtraFields) != 1 || drift.ExtraFields[0] != "new_field" {
		t.Fatal(drift, err)
	}
	target, err := r.WithIndex(index + "*")
	if err != nil {
		t.Fatal(err)
	}
	hits, err := target.Find().Size(1).All(ctx)
	if err != nil || len(hits) != 1 || hits[0].Index != index {
		t.Fatal(hits, err)
	}
}
