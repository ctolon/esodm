//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ctolon/esodm"
	"github.com/ctolon/esodm/adapter/es8"
	"github.com/ctolon/esodm/adapter/es9"
	elastic8 "github.com/elastic/go-elasticsearch/v8"
	util8 "github.com/elastic/go-elasticsearch/v8/esutil"
	elastic9 "github.com/elastic/go-elasticsearch/v9"
	util9 "github.com/elastic/go-elasticsearch/v9/esutil"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/deletebyquery"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/update"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/updatebyquery"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

type featureDocument struct {
	esodm.Timestamps
	esodm.DocumentMeta
	Category string         `json:"category"`
	Color    string         `json:"color"`
	Price    int            `json:"price"`
	Point    esodm.GeoPoint `json:"point"`
}

func featureRepository(t *testing.T, c *esodm.Client, index string) *esodm.Repository[featureDocument] {
	t.Helper()
	s, err := esodm.NewSchema[featureDocument](index)
	if err != nil {
		t.Fatal(err)
	}
	r, err := esodm.NewRepository(c, s)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.EnsureIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	return r
}
func TestFeatureModelsFacetsAndOperations(t *testing.T) {
	c := client(t)
	ctx := context.Background()
	index := name()
	cleanupIndex(t, c, index)
	r := featureRepository(t, c, index)
	docs := []featureDocument{{DocumentMeta: esodm.DocumentMeta{ID: "1", Routing: "tenant"}, Category: "books", Color: "red", Point: esodm.GeoPoint{Lat: 41, Lon: 29}}, {DocumentMeta: esodm.DocumentMeta{ID: "2", Routing: "tenant"}, Category: "books", Color: "blue", Point: esodm.GeoPoint{Lat: 41, Lon: 29}}, {DocumentMeta: esodm.DocumentMeta{ID: "3", Routing: "tenant"}, Category: "games", Color: "red", Point: esodm.GeoPoint{Lat: 41, Lon: 29}}}
	for _, doc := range docs {
		if _, err := r.Save(ctx, doc, esodm.WriteOptions{Refresh: "wait_for"}); err != nil {
			t.Fatal(err)
		}
	}
	hit, err := r.Get(ctx, "1", "tenant")
	if err != nil || hit.Source.CreatedAt.IsZero() {
		t.Fatal(hit, err)
	}
	facets := map[string]esodm.Facet{"category": {Aggregation: esodm.TermsAgg("category", 10, nil), Selection: esodm.Term("category", "books")}, "color": {Aggregation: esodm.TermsAgg("color", 10, nil), Selection: esodm.Term("color", "red")}}
	result, err := r.Search(ctx, esodm.WithFacets(esodm.NewSearch(esodm.MatchAll()).Routing("tenant").TypedKeys(true).With("stored_fields", []string{"_routing"}).With("_source", true), facets))
	if err != nil || result.Total() != 1 {
		t.Fatal(result, err)
	}
	if result.Hits.Hits[0].Source.DocumentRouting() != "tenant" {
		t.Fatal("routing hydration", result)
	}
	values, err := esodm.DecodeFacet[struct {
		Buckets []struct {
			Key   string
			Count int `json:"doc_count"`
		}
	}](result.Aggregations, "category")
	if err != nil || len(values.Buckets) != 2 {
		t.Fatal(values, err)
	}
	for _, b := range values.Buckets {
		if b.Count != 1 {
			t.Fatal(values)
		}
	}
	geo, err := r.Search(ctx, esodm.NewSearch(esodm.Geo("point").WithinDistance("1km", esodm.GeoPoint{Lat: 41, Lon: 29})).Routing("tenant"))
	if err != nil || geo.Total() != 3 {
		t.Fatal(geo, err)
	}
	updated, err := r.UpdateWith(ctx, "1", &update.Request{Script: &types.Script{Source: "ctx._source.price = 42"}, Source_: true}, esodm.UpdateOptions{WriteOptions: esodm.WriteOptions{Routing: "tenant", Refresh: "wait_for"}, RetryOnConflict: 2})
	if err != nil || updated.Get == nil || updated.Get.Source.Price != 42 {
		t.Fatal(updated, err)
	}
	query := &types.Query{MatchAll: &types.MatchAllQuery{}}
	batch, err := r.UpdateByQuery(ctx, &updatebyquery.Request{Query: query, Script: &types.Script{Source: "ctx._source.price += 1"}}, esodm.ByQueryOptions{Routing: []string{"tenant"}, Refresh: true})
	if err != nil || batch.Updated == nil || *batch.Updated != 3 {
		t.Fatal(batch, err)
	}
	deletion, err := r.DeleteByQuery(ctx, &deletebyquery.Request{Query: esodmQuery(t, esodm.Term("category", "games"))}, esodm.ByQueryOptions{Routing: []string{"tenant"}, Refresh: true, Async: true})
	if err != nil || deletion.Task == nil {
		t.Fatal(deletion, err)
	}
	task, err := c.Admin().WaitTask(ctx, *deletion.Task, 10*time.Millisecond)
	if err != nil || !task.Completed {
		t.Fatal(task, err)
	}
	count, err := r.Count(ctx, esodm.MatchAll())
	if err != nil || count != 2 {
		t.Fatal(count, err)
	}
	target := name()
	cleanupIndex(t, c, target)
	destination := featureRepository(t, c, target)
	seq := func(yield func(esodm.BulkOperation[featureDocument], error) bool) {
		for h, err := range r.Each(ctx, esodm.NewSearch(esodm.MatchAll()).Routing("tenant"), 1, "1m") {
			if !yield(esodm.BulkOperation[featureDocument]{Action: esodm.BulkIndex, ID: h.ID, Document: h.Source}, err) {
				return
			}
		}
	}
	if err = destination.BulkSeq2(ctx, seq, esodm.BulkStreamOptions{BatchSize: 1, Refresh: "wait_for"}, func(result esodm.BulkBatchResult) error { return result.Err }); err != nil {
		t.Fatal(err)
	}
	if n, err := destination.Count(ctx, esodm.MatchAll()); err != nil || n != 2 {
		t.Fatal(n, err)
	}
}
func esodmQuery(t *testing.T, q esodm.Query) *types.Query {
	t.Helper()
	raw, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	var query types.Query
	if err = json.Unmarshal(raw, &query); err != nil {
		t.Fatal(err)
	}
	return &query
}
func TestFeaturePercolateAndIndexer(t *testing.T) {
	c := client(t)
	ctx := context.Background()
	index := name()
	cleanupIndex(t, c, index)
	type rule struct {
		Query    json.RawMessage `json:"query" es:"type=percolator"`
		Category string          `json:"category"`
	}
	schema, err := esodm.NewSchema[rule](index)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := esodm.NewRepository(c, schema)
	if err = r.EnsureIndex(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Create(ctx, "rule", rule{Query: json.RawMessage(`{"term":{"category":"books"}}`)}, esodm.WriteOptions{Refresh: "wait_for"}); err != nil {
		t.Fatal(err)
	}
	result, err := r.Search(ctx, esodm.NewSearch(esodm.Percolate(types.PercolateQuery{Field: "query", Document: json.RawMessage(`{"category":"books"}`)})))
	if err != nil || result.Total() != 1 {
		t.Fatal(result, err)
	}
	target := name()
	cleanupIndex(t, c, target)
	dest := featureRepository(t, c, target)
	results := make(chan esodm.BulkItem, 1)
	op := esodm.BulkOperation[featureDocument]{Action: esodm.BulkCreate, Document: featureDocument{Point: esodm.GeoPoint{Lat: 0, Lon: 0}}}
	// A plain model with no Identified implementation supports server-generated bulk IDs.
	op.ID = "adapter-id"
	callback := func(_ context.Context, item esodm.BulkItem) { results <- item }
	switch raw := c.Transport().(type) {
	case *elastic8.Client:
		indexer, err := util8.NewBulkIndexer(util8.BulkIndexerConfig{Client: raw, NumWorkers: 1, Refresh: "wait_for"})
		if err != nil {
			t.Fatal(err)
		}
		item, err := es8.BulkIndexerItem(ctx, dest, op, callback)
		if err != nil {
			t.Fatal(err)
		}
		if err = indexer.Add(ctx, item); err != nil {
			t.Fatal(err)
		}
		if err = indexer.Close(ctx); err != nil {
			t.Fatal(err)
		}
	case *elastic9.Client:
		indexer, err := util9.NewBulkIndexer(util9.BulkIndexerConfig{Client: raw, NumWorkers: 1, Refresh: "wait_for"})
		if err != nil {
			t.Fatal(err)
		}
		item, err := es9.BulkIndexerItem(ctx, dest, op, callback)
		if err != nil {
			t.Fatal(err)
		}
		if err = indexer.Add(ctx, item); err != nil {
			t.Fatal(err)
		}
		if err = indexer.Close(ctx); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unexpected transport %T", raw)
	}
	select {
	case item := <-results:
		if item.Err != nil || item.ID != "adapter-id" {
			t.Fatal(item)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("missing callback")
	}
	if n, err := dest.Count(ctx, esodm.MatchAll()); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
func TestFeatureAsyncMigration(t *testing.T) {
	c := client(t)
	ctx := context.Background()
	source, target, alias := name(), name(), name()
	cleanupIndex(t, c, source)
	cleanupIndex(t, c, target)
	r := featureRepository(t, c, source)
	if _, err := r.Create(ctx, "one", featureDocument{Point: esodm.GeoPoint{Lat: 41, Lon: 29}}, esodm.WriteOptions{Refresh: "wait_for"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Admin().Aliases(ctx, esodm.AliasAction{Add: true, Index: source, Alias: alias}); err != nil {
		t.Fatal(err)
	}
	schema, err := esodm.NewSchema[featureDocument](target)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := esodm.PlanMigration(ctx, c, alias, target, schema)
	if err != nil {
		t.Fatal(err)
	}
	var saved []byte
	options := esodm.MigrationOptions{WritesPaused: true, PollInterval: 10 * time.Millisecond, Checkpoint: func(state esodm.MigrationState) error { var err error; saved, err = json.Marshal(state); return err }}
	state, err := c.Admin().StartMigration(ctx, plan, options)
	if err != nil {
		t.Fatal(state, err)
	}
	if state.TaskID == "" {
		t.Fatal(state)
	}
	var resumed esodm.MigrationState
	if err = json.Unmarshal(saved, &resumed); err != nil {
		t.Fatal(err)
	}
	resumed, err = c.Admin().ResumeMigration(ctx, resumed, options)
	if err != nil || resumed.Phase != "complete" || resumed.Documents != 1 {
		t.Fatal(resumed, err)
	}
	if _, err = c.Admin().ResumeMigration(ctx, resumed, options); err != nil {
		t.Fatal(err)
	}
	var aliases map[string]any
	if err = c.Do(ctx, "GET", "/_alias/"+alias, nil, nil, &aliases); err != nil || aliases[target] == nil {
		t.Fatal(fmt.Sprint(aliases), err)
	}
}
