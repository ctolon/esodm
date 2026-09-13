//go:build integration

package integration_test

import (
	"context"
	"testing"

	"github.com/ctolon/esodm"
	search8 "github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	search9 "github.com/elastic/go-elasticsearch/v9/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v9/typedapi/esdsl"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

func TestOfficialMappingAndRequests(t *testing.T) {
	type document struct {
		Title  string    `json:"title"`
		Vector []float32 `json:"vector"`
	}
	c := client(t)
	ctx := context.Background()
	index := name()
	cleanupIndex(t, c, index)
	mapping := esdsl.NewTypeMapping().Properties(map[string]types.Property{
		"title":  esdsl.NewTextProperty().TextPropertyCaster(),
		"vector": esdsl.NewDenseVectorProperty().Dims(3).DenseVectorPropertyCaster(),
	}).TypeMappingCaster()
	schema, err := esodm.SchemaFromMapping[document](index, mapping, nil)
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
	if _, err := r.Create(ctx, "1", document{"Go book", []float32{1, 2, 3}}, esodm.WriteOptions{Refresh: "wait_for"}); err != nil {
		t.Fatal(err)
	}
	search := esodm.SearchFromRequest(&search9.Request{Query: esdsl.NewMatchQuery("title", "Go").QueryCaster()})
	result, err := r.Search(ctx, search)
	if err != nil || result.Hits.Total.Value != 1 || len(result.Hits.Hits[0].Source.Vector) != 3 {
		t.Fatalf("typed search/vector hydration: %+v %v", result, err)
	}
	var builder esodm.RequestBuilder = search8.New(nil).Index(index)
	if c.Version().Major == 9 {
		builder = search9.New(nil).Index(index)
	}
	var raw esodm.SearchResult[document]
	if err := c.DoTyped(ctx, builder, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Hits.Total.Value != 1 || raw.Hits.Hits[0].Source.Title != "Go book" {
		t.Fatalf("native request: %+v", raw)
	}
}
