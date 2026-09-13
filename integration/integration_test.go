//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/ctolon/esodm"
	"github.com/ctolon/esodm/adapter/es8"
	"github.com/ctolon/esodm/adapter/es9"
	"github.com/elastic/elastic-transport-go/v8/elastictransport"
	elastic8 "github.com/elastic/go-elasticsearch/v8"
	elastic9 "github.com/elastic/go-elasticsearch/v9"
)

var testURL = flag.String("es-url", os.Getenv("ESODM_TEST_URL"), "test server URL")
var testMajor = flag.String("es-major", os.Getenv("ESODM_TEST_MAJOR"), "test server major")
var testLicensed = flag.Bool("es-licensed", os.Getenv("ESODM_TEST_LICENSED") == "1", "run licensed scenarios")

func client(t *testing.T) *esodm.Client {
	t.Helper()
	address := *testURL
	major := *testMajor
	if address == "" || major == "" {
		t.Fatal("integration tests require ESODM_TEST_URL and ESODM_TEST_MAJOR (8 or 9)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var c *esodm.Client
	var err error
	switch major {
	case "8":
		raw, e := elastic8.NewClient(elastic8.Config{Addresses: []string{address}, DisableRetry: true})
		if e != nil {
			t.Fatal(e)
		}
		c, err = es8.Connect(ctx, raw, esodm.Config{})
	case "9":
		raw, e := elastic9.New(elastic9.WithAddresses(address), elastic9.WithTransportOptions(elastictransport.WithDisableRetry()))
		if e != nil {
			t.Fatal(e)
		}
		c, err = es9.Connect(ctx, raw, esodm.Config{})
	default:
		t.Fatal("invalid ESODM_TEST_MAJOR")
	}
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func name() string { return "esodm-it-" + strconv.FormatInt(time.Now().UnixNano(), 36) }
func cleanupIndex(t *testing.T, c *esodm.Client, index string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.Admin().DeleteIndex(ctx, index); err != nil && !errors.Is(err, esodm.ErrNotFound) {
			t.Errorf("cleanup %s: %v", index, err)
		}
	})
}

type comment struct {
	Text   string `json:"text" es:"type=text"`
	Rating int    `json:"rating"`
}
type product struct {
	Title       string               `json:"title" es:"type=text"`
	Category    string               `json:"category"`
	Price       int                  `json:"price"`
	Description *string              `json:"description"`
	Comments    []comment            `json:"comments" es:"type=nested"`
	Vector      []float32            `json:"vector" es:"type=dense_vector"`
	Sparse      map[string]float32   `json:"sparse,omitempty" es:"type=sparse_vector"`
	Related     []esodm.Ref[product] `json:"related"`
}

func repo(t *testing.T, c *esodm.Client, index string) *esodm.Repository[product] {
	t.Helper()
	s, err := esodm.NewSchema[product](index, esodm.SchemaConfig{Properties: map[string]esodm.FieldMapping{"vector": {Type: "dense_vector", Dims: 3, Similarity: "cosine"}}, Settings: map[string]any{"number_of_shards": 1, "number_of_replicas": 0}})
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
func TestDocumentsAndSearch(t *testing.T) {
	c := client(t)
	ctx := context.Background()
	index := name()
	cleanupIndex(t, c, index)
	r := repo(t, c, index)
	for i := 1; i <= 3; i++ {
		doc := product{Title: "Go book", Category: "books", Price: i * 10, Comments: []comment{{"excellent", i}}, Vector: []float32{1, float32(i), 1}, Sparse: map[string]float32{"go": float32(i)}, Related: []esodm.Ref[product]{{Index: index, ID: "1"}}}
		if _, err := r.Create(ctx, strconv.Itoa(i), doc, esodm.WriteOptions{Refresh: "wait_for"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Create(ctx, "1", product{}, esodm.WriteOptions{}); !errors.Is(err, esodm.ErrConflict) {
		t.Fatal(err)
	}
	hit, err := r.Get(ctx, "1", "")
	if err != nil || hit.Source.Price != 10 || hit.SeqNo == nil || len(hit.Source.Vector) != 3 || len(hit.Source.Sparse) != 1 {
		t.Fatal(hit, err)
	}
	if _, err = r.Replace(ctx, "1", hit.Source, esodm.WriteOptions{IfSeqNo: hit.SeqNo, IfPrimaryTerm: hit.PrimaryTerm}); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Replace(ctx, "1", hit.Source, esodm.WriteOptions{IfSeqNo: hit.SeqNo, IfPrimaryTerm: hit.PrimaryTerm}); !errors.Is(err, esodm.ErrConflict) {
		t.Fatal(err)
	}
	if _, err = r.Update(ctx, "1", esodm.Patch{"price": 0, "description": nil}, esodm.WriteOptions{Refresh: "wait_for"}); err != nil {
		t.Fatal(err)
	}
	hit, err = r.Get(ctx, "1", "")
	if err != nil || hit.Source.Price != 0 || hit.Source.Description != nil || hit.Source.Title != "Go book" {
		t.Fatal(hit, err)
	}
	if found, err := r.Exists(ctx, "missing", ""); err != nil || found {
		t.Fatal(found, err)
	}
	refs, err := r.MGet(ctx, []string{"1", "missing", "1"}, "")
	if err != nil || len(refs) != 3 || refs[1].Found || refs[2].Hit.ID != "1" {
		t.Fatal(refs, err)
	}
	graph, err := esodm.Preload(ctx, c, []esodm.Ref[product]{{Index: index, ID: "1"}}, func(p product) []esodm.Ref[product] { return p.Related }, esodm.PreloadOptions{})
	if err != nil || len(graph.Nodes) != 1 {
		t.Fatal(graph, err)
	}
	search := esodm.NewSearch(esodm.And(esodm.Match("title", "Go"), esodm.Term("category", "books"))).Size(2).Sort(esodm.Sort{Field: "price"}).Highlight("title").Aggregations(map[string]esodm.Aggregation{"categories": esodm.TermsAgg("category", 10, map[string]esodm.Aggregation{"avg_price": esodm.Avg("price")})})
	result, err := r.Search(ctx, search)
	if err != nil || result.Hits.Total.Value != 3 || len(result.Hits.Hits) != 2 || len(result.Aggregations) == 0 || len(result.Hits.Hits[0].Highlight) == 0 {
		t.Fatal(result, err)
	}
	nested, err := r.Search(ctx, esodm.NewSearch(esodm.Nested("comments", esodm.OrderedValue[int]("comments.rating").GTE(2))))
	if err != nil || nested.Hits.Total.Value != 2 {
		t.Fatal(nested, err)
	}
	results, err := r.MultiSearch(ctx, search, esodm.NewSearch(esodm.MatchNone()))
	if err != nil || len(results) != 2 || results[0].Err != nil || results[1].Err != nil || results[1].Result.Hits.Total.Value != 0 {
		t.Fatal(results, err)
	}
	it, err := r.Iterate(ctx, esodm.NewSearch(esodm.MatchAll()), 1, "")
	if err != nil {
		t.Fatal(err)
	}
	defer it.Close()
	seen := map[string]bool{}
	for it.Next() {
		id := it.Hit().ID
		if seen[id] {
			t.Fatal("duplicate hit", id)
		}
		seen[id] = true
	}
	if it.Err() != nil || len(seen) != 3 {
		t.Fatal(seen, it.Err())
	}
	knn, err := r.Search(ctx, esodm.Search{}.KNN(esodm.KNN{Field: "vector", Vector: []float32{1, 1, 1}, K: 2, Candidates: 3}).Size(2))
	if err != nil || len(knn.Hits.Hits) != 2 || knn.Hits.Hits[0].ID != "1" {
		t.Fatal(knn, err)
	}
	sparse, err := r.Search(ctx, esodm.NewSearch(esodm.SparseVector("sparse", map[string]float32{"go": 1})))
	if err != nil || len(sparse.Hits.Hits) != 3 {
		t.Fatal(sparse, err)
	}
	runtime, err := r.Search(ctx, esodm.NewSearch(esodm.Term("kind", "book")).With("fields", []string{"kind"}).Runtime(map[string]any{"kind": map[string]any{"type": "keyword", "script": "emit('book')"}}))
	if err != nil || runtime.Hits.Total.Value != 3 || len(runtime.Hits.Hits[0].Fields["kind"]) == 0 {
		t.Fatal(runtime, err)
	}
	bulk, err := r.Bulk(ctx, []esodm.BulkOperation[product]{{Action: esodm.BulkUpdate, ID: "2", Patch: esodm.Patch{"price": 22}}, {Action: esodm.BulkCreate, ID: "1", Document: hit.Source}, {Action: esodm.BulkDelete, ID: "3"}}, "wait_for")
	var be *esodm.BulkError
	if !errors.As(err, &be) || be.Failed != 1 || !errors.Is(bulk.Items[1].Err, esodm.ErrConflict) {
		t.Fatal(bulk, err)
	}
	if count, err := r.Count(ctx, esodm.MatchAll()); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	if _, err = r.Upsert(ctx, "4", esodm.Patch{"price": 4}, hit.Source, esodm.WriteOptions{Refresh: "wait_for"}); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Delete(ctx, "4", esodm.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
}
func TestEscapedIDs(t *testing.T) {
	c := client(t)
	ctx := context.Background()
	index := name()
	cleanupIndex(t, c, index)
	r := repo(t, c, index)
	for _, id := range []string{"文書/a b?c#d", ".", ".."} {
		doc := product{Title: id, Vector: []float32{1, 1, 1}}
		if _, err := r.Create(ctx, id, doc, esodm.WriteOptions{}); err != nil {
			t.Fatal(id, err)
		}
		hit, err := r.Get(ctx, id, "")
		if err != nil || hit.ID != id || hit.Source.Title != id {
			t.Fatal(id, hit, err)
		}
		if _, err := r.Delete(ctx, id, esodm.WriteOptions{}); err != nil {
			t.Fatal(id, err)
		}
	}
}

func TestParentChild(t *testing.T) {
	c := client(t)
	ctx := context.Background()
	index := name()
	cleanupIndex(t, c, index)
	type doc struct {
		Name string     `json:"name"`
		Link esodm.Join `json:"link" es:"type=join"`
	}
	s, err := esodm.NewSchema[doc](index, esodm.SchemaConfig{Properties: map[string]esodm.FieldMapping{"link": {Type: "join", Relations: map[string]any{"parent": "child"}}}, Settings: map[string]any{"number_of_shards": 2, "number_of_replicas": 0}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := esodm.NewRepository(c, s)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.EnsureIndex(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Create(ctx, "p", doc{"parent", esodm.Join{Name: "parent"}}, esodm.WriteOptions{Routing: "p"}); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Create(ctx, "c", doc{"child", esodm.Join{Name: "child", Parent: "p"}}, esodm.WriteOptions{}); !errors.Is(err, esodm.ErrValidation) {
		t.Fatal(err)
	}
	if _, err = r.Create(ctx, "c", doc{"child", esodm.Join{Name: "child", Parent: "p"}}, esodm.WriteOptions{Routing: "p", Refresh: "wait_for"}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []esodm.Query{esodm.HasChild("child", esodm.MatchAll()), esodm.HasParent("parent", esodm.MatchAll()), esodm.ParentID("child", "p")} {
		res, err := r.Search(ctx, esodm.NewSearch(q))
		if err != nil || res.Hits.Total.Value != 1 {
			t.Fatal(res, err)
		}
	}
	if _, err = r.Get(ctx, "c", "p"); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Delete(ctx, "c", esodm.WriteOptions{Routing: "p"}); err != nil {
		t.Fatal(err)
	}
}
func TestMigration(t *testing.T) {
	c := client(t)
	ctx := context.Background()
	source, target, alias := name(), name(), name()
	cleanupIndex(t, c, source)
	cleanupIndex(t, c, target)
	r := repo(t, c, source)
	if _, err := r.Create(ctx, "1", product{Title: "Go", Vector: []float32{1, 1, 1}}, esodm.WriteOptions{Refresh: "wait_for"}); err != nil {
		t.Fatal(err)
	}
	w := true
	if err := c.Admin().Aliases(ctx, esodm.AliasAction{Add: true, Index: source, Alias: alias, WriteIndex: &w}); err != nil {
		t.Fatal(err)
	}
	plan, err := esodm.PlanMigration(ctx, c, alias, target, r.Schema())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Admin().ApplyMigration(ctx, plan, esodm.MigrationOptions{}); !errors.Is(err, esodm.ErrValidation) {
		t.Fatal(err)
	}
	result, err := c.Admin().ApplyMigration(ctx, plan, esodm.MigrationOptions{WritesPaused: true})
	if err != nil || !result.AliasMoved || result.Documents != 1 {
		t.Fatal(result, err)
	}
	if _, err = r.Get(ctx, "1", ""); err != nil {
		t.Fatal("source was not retained", err)
	}
	if _, err = c.Admin().ApplyMigration(ctx, plan, esodm.MigrationOptions{WritesPaused: true}); !errors.Is(err, esodm.ErrConflict) {
		t.Fatal(err)
	}
}
func TestManagement(t *testing.T) {
	c := client(t)
	ctx := context.Background()
	a := c.Admin()
	prefix := name()
	pipeline := prefix + "-pipe"
	policy := prefix + "-policy"
	component := prefix + "-component"
	template := prefix + "-template"
	stream := prefix + "-stream"
	resources := []struct {
		k    esodm.ResourceKind
		n    string
		spec any
	}{
		{esodm.IngestPipeline, pipeline, map[string]any{"processors": []any{map[string]any{"set": map[string]any{"field": "tag", "value": "processed"}}}}},
		{esodm.LifecyclePolicy, policy, map[string]any{"policy": map[string]any{"phases": map[string]any{"hot": map[string]any{"actions": map[string]any{"rollover": map[string]any{"max_docs": 1000}}}}}}},
		{esodm.ComponentTemplate, component, map[string]any{"template": map[string]any{"settings": map[string]any{"number_of_shards": 1, "number_of_replicas": 0}, "mappings": map[string]any{"properties": map[string]any{"@timestamp": map[string]any{"type": "date"}}}}}},
		{esodm.IndexTemplate, template, map[string]any{"index_patterns": []string{stream + "*"}, "data_stream": map[string]any{}, "composed_of": []string{component}, "priority": 500}},
		{esodm.DataStream, stream, nil},
	}
	for _, resource := range resources {
		if err := a.Put(ctx, resource.k, resource.n, resource.spec); err != nil {
			t.Fatal(resource.k, err)
		}
		t.Cleanup(func() {
			if err := a.Delete(context.Background(), resource.k, resource.n); err != nil {
				t.Error(err)
			}
		})
		if _, err := a.Get(ctx, resource.k, resource.n); err != nil {
			t.Fatal(err)
		}
	}
	sim, err := a.SimulatePipeline(ctx, pipeline, []any{map[string]string{"title": "Go"}})
	if err != nil || !json.Valid(sim) {
		t.Fatal(string(sim), err)
	}
	var written any
	if err = c.Do(ctx, "PUT", "/"+stream+"/_create/1", nil, map[string]any{"@timestamp": time.Now().UTC().Format(time.RFC3339)}, &written); err != nil {
		t.Fatal(err)
	}
	roll, err := a.Rollover(ctx, stream, map[string]any{"max_docs": 1}, true)
	if err != nil || !roll.DryRun {
		t.Fatal(roll, err)
	}
	if _, err := a.ExplainLifecycle(ctx, stream); err != nil {
		t.Fatal(err)
	}
}
func TestLicensedHybrid(t *testing.T) {
	if !*testLicensed {
		t.Skip("requires licensed test job: ESODM_TEST_LICENSED=1")
	}
	c := client(t)
	ctx := context.Background()
	index := name()
	cleanupIndex(t, c, index)
	r := repo(t, c, index)
	if _, err := r.Create(ctx, "1", product{Title: "Go", Vector: []float32{1, 1, 1}}, esodm.WriteOptions{Refresh: "wait_for"}); err != nil {
		t.Fatal(err)
	}
	result, err := r.Search(ctx, esodm.NewSearch(esodm.MatchAll()).Size(1).HybridRRF(esodm.Match("title", "Go"), esodm.KNN{Field: "vector", Vector: []float32{1, 1, 1}, K: 1, Candidates: 2}, 10, 60))
	if err != nil || len(result.Hits.Hits) != 1 {
		t.Fatal(fmt.Sprint(result), err)
	}
}

func TestReviewSearchAndCursor(t *testing.T) {
	c := client(t)
	ctx := context.Background()
	index := name()
	cleanupIndex(t, c, index)
	type Base struct {
		Created time.Time `json:"created"`
	}
	type Document struct {
		Base
		Value int `json:"value"`
	}
	schema, err := esodm.NewSchema[Document](index)
	if err != nil {
		t.Fatal(err)
	}
	r, err := esodm.NewRepository(c, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.EnsureIndex(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	for i := 1; i <= 3; i++ {
		if _, err = r.Create(ctx, strconv.Itoa(i), Document{Base: Base{Created: now}, Value: i}, esodm.WriteOptions{Routing: "route", Refresh: "wait_for"}); err != nil {
			t.Fatal(err)
		}
	}
	search := esodm.NewSearch(esodm.Date("created").GTE(now)).Routing("route").TypedKeys(true).Aggregations(map[string]esodm.Aggregation{"average": esodm.Avg("value")})
	result, err := r.Search(ctx, search)
	if err != nil || result.Total() != 3 {
		t.Fatal(result, err)
	}
	if aggregates, err := result.Aggregates(); err != nil || aggregates["average"] == nil {
		t.Fatal(aggregates, err)
	}
	multi, err := r.MultiSearch(ctx, search.RequestCache(true))
	if err != nil || multi[0].Err != nil || multi[0].Result.Total() != 3 {
		t.Fatal(multi, err)
	}
	count, err := r.CountWith(ctx, esodm.MatchAll(), esodm.CountOptions{Routing: []string{"route"}, Preference: "session"})
	if err != nil || count != 3 {
		t.Fatal(count, err)
	}
	ordered := esodm.NewSearch(esodm.MatchAll()).Sort(esodm.NewField[int]("value").Asc())
	it, err := r.Iterate(ctx, ordered.Routing("route"), 2, "1m")
	if err != nil {
		t.Fatal(err)
	}
	defer it.Close()
	if !it.Next() || it.Hit().Source.Value != 1 {
		t.Fatal(it.Hit(), it.Err())
	}
	cursor, err := it.Detach()
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := r.ResumeIterator(ctx, ordered, cursor, 2, "1m")
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	want := 2
	for hit, err := range resumed.Each() {
		if err != nil || hit.Source.Value != want {
			t.Fatal(hit, err, want)
		}
		want++
	}
	if want != 4 {
		t.Fatal(want)
	}
}
