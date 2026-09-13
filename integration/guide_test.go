//go:build integration

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ctolon/esodm"
	"github.com/ctolon/esodm/examples/guide"
)

func TestGuideWorkflow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c := client(t)
	index := name()
	schema, err := esodm.NewSchema[guide.Product](index)
	if err != nil {
		t.Fatal(err)
	}
	r, err := esodm.NewRepository(c, schema)
	if err != nil {
		t.Fatal(err)
	}
	cleanupIndex(t, c, index)
	if err = guide.Initialize(ctx, r); err != nil {
		t.Fatal(err)
	}
	if _, err = guide.CreateProduct(ctx, r); err != nil {
		t.Fatal(err)
	}
	if _, err = guide.CreateProduct(ctx, r); !errors.Is(err, esodm.ErrConflict) {
		t.Fatal(err)
	}
	if _, err = guide.ChangePrice(ctx, r, "book-1", "", 28); err != nil {
		t.Fatal(err)
	}
	if err = c.Admin().Refresh(ctx, index); err != nil {
		t.Fatal(err)
	}
	found, err := guide.FindBooks(ctx, r)
	if err != nil || found.Total() != 1 || found.Sources()[0].Price != 28 {
		t.Fatal(found, err)
	}
	buckets, err := guide.FacetBooks(ctx, r)
	if err != nil || len(buckets.Buckets) != 1 || buckets.Buckets[0].Count != 1 {
		t.Fatal(buckets, err)
	}
	if _, err = guide.SaveProduct(ctx, r); err != nil {
		t.Fatal(err)
	}
	seq := func(yield func(esodm.BulkOperation[guide.Product], error) bool) {
		for i := range 2 {
			if !yield(esodm.BulkOperation[guide.Product]{Action: esodm.BulkCreate, ID: fmt.Sprintf("bulk-%d", i), Document: guide.Product{Name: "Go", Category: "books", Price: 10}}, nil) {
				return
			}
		}
	}
	if err = guide.ImportProducts(ctx, r, seq); err != nil {
		t.Fatal(err)
	}
	if err = c.Admin().Refresh(ctx, index); err != nil {
		t.Fatal(err)
	}
	count := 0
	if err = guide.VisitProducts(ctx, r, func(guide.Product) error { count++; return nil }); err != nil || count != 4 {
		t.Fatal(count, err)
	}
	if _, err = guide.IncrementPrice(ctx, r, "book-1"); err != nil {
		t.Fatal(err)
	}
	// By-query takes a search snapshot; refresh the preceding script first.
	if err = c.Admin().Refresh(ctx, index); err != nil {
		t.Fatal(err)
	}
	task, err := guide.RepriceBooks(ctx, r)
	if err != nil || task.Task == nil {
		t.Fatal(task, err)
	}
	completed, err := c.Admin().WaitTask(ctx, *task.Task, 10*time.Millisecond)
	if err != nil || !completed.Completed {
		t.Fatal(completed, err)
	}
	if err = c.Admin().Refresh(ctx, index); err != nil {
		t.Fatal(err)
	}
	removed, err := guide.DeleteCategory(ctx, r, "books")
	if err != nil || removed.Deleted == nil || *removed.Deleted != 3 {
		t.Fatal(removed, err)
	}
}
