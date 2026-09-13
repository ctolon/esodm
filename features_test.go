package esodm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/elastic/go-elasticsearch/v9/typedapi/core/deletebyquery"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/update"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/updatebyquery"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types/enums/conflicts"
)

type featureModel struct {
	*Timestamps
	DocumentMeta
	Name string `json:"name"`
}

func (*featureModel) IndexName() string { return "features" }
func TestModelLifecycle(t *testing.T) {
	s, err := NewSchemaFor[featureModel]()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(s.Mapping()), "created_at") || strings.Contains(string(s.Mapping()), "routing") {
		t.Fatal(string(s.Mapping()))
	}
	now := time.Date(2026, 9, 6, 1, 2, 3, 0, time.UTC)
	var bodies []map[string]json.RawMessage
	c := testClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("routing") != "tenant" {
			t.Fatal(req.URL)
		}
		bodies = append(bodies, readBody(t, req))
		if strings.HasSuffix(req.URL.Path, "/_doc") {
			if req.Method != "POST" || req.URL.Query().Get("op_type") != "create" {
				t.Fatal(req.URL)
			}
		}
		return response(201, `{"_id":"generated","result":"created"}`), nil
	})
	c.config.Now = func() time.Time { return now }
	r, _ := NewRepository(c, s)
	doc := featureModel{DocumentMeta: DocumentMeta{ID: "x/y", Routing: "tenant"}, Name: "Ada"}
	if _, err = r.Save(context.Background(), doc, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if doc.Timestamps != nil {
		t.Fatal("caller embed mutated")
	}
	if _, err := r.Insert(context.Background(), doc, WriteOptions{}); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	doc.ID = ""
	if got, err := r.Insert(context.Background(), doc, WriteOptions{}); err != nil || got.ID != "generated" {
		t.Fatal(got, err)
	}
	patch := Patch{"name": "Grace"}
	if _, err = r.Update(context.Background(), "x", patch, WriteOptions{Routing: "tenant"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := patch["updated_at"]; ok {
		t.Fatal("patch mutated")
	}
	if !bytes.Contains(bodies[0]["created_at"], []byte("2026-09-06")) || !bytes.Contains(bodies[2]["doc"], []byte("updated_at")) {
		t.Fatal(bodies)
	}
	if _, err = NewSchemaFor[testDoc](); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	if _, err = NewSchemaFor[*featureModel](); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	ordinary := testRepo(t, func(*http.Request) (*http.Response, error) { t.Fatal("unexpected"); return nil, nil })
	if _, err = ordinary.Save(context.Background(), testDoc{}, WriteOptions{}); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	seq, term := int64(1), int64(1)
	if _, err = r.Insert(context.Background(), doc, WriteOptions{IfSeqNo: &seq, IfPrimaryTerm: &term}); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	r.hooks.Validate = func(context.Context, *featureModel) error { return io.ErrUnexpectedEOF }
	if _, err = r.Upsert(context.Background(), "x", Patch{}, doc, WriteOptions{}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
}

type featureEnsurer struct {
	name  string
	calls *[]string
	err   error
}

func (e *featureEnsurer) Index() string { return e.name }
func (e *featureEnsurer) EnsureIndex(context.Context) error {
	*e.calls = append(*e.calls, e.name)
	return e.err
}
func TestRegistry(t *testing.T) {
	var calls []string
	var registry Registry
	a := &featureEnsurer{"one", &calls, nil}
	b := &featureEnsurer{"two", &calls, io.EOF}
	if err := registry.Add(a, b); err != nil {
		t.Fatal(err)
	}
	names := registry.Indexes()
	names[0] = "mutated"
	if registry.Indexes()[0] != "one" {
		t.Fatal(names)
	}
	for _, entry := range []Ensurer{a, (*featureEnsurer)(nil), &featureEnsurer{name: "a/b"}} {
		if err := registry.Add(entry); !errors.Is(err, ErrValidation) {
			t.Fatal(err)
		}
	}
	if err := registry.EnsureAll(context.Background()); !errors.Is(err, io.EOF) || strings.Join(calls, ",") != "one,two" {
		t.Fatal(calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := registry.EnsureAll(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestBulkRetriesPrepareOnce(t *testing.T) {
	attempts, writes, after := 0, 0, 0
	r := testRepo(t, func(req *http.Request) (*http.Response, error) {
		attempts++
		body, _ := io.ReadAll(req.Body)
		if attempts == 1 {
			return response(200, `{"items":[{"index":{"_id":"1","status":201}},{"index":{"_id":"2","status":429,"error":{"type":"busy","reason":"busy"}}}]}`), nil
		}
		if bytes.Contains(body, []byte(`"_id":"1"`)) || !bytes.Contains(body, []byte(`"_id":"2"`)) {
			t.Fatal(string(body))
		}
		return response(200, `{"items":[{"index":{"_id":"2","status":201}}]}`), nil
	})
	r.hooks.BeforeWrite = func(context.Context, Operation, *testDoc) error { writes++; return nil }
	r.hooks.AfterWrite = func(context.Context, Operation, WriteResult) error { after++; return nil }
	ops := []BulkOperation[testDoc]{{Action: BulkIndex, ID: "1"}, {Action: BulkIndex, ID: "2"}}
	result, err := r.BulkWithOptions(context.Background(), ops, BulkOptions{Retry: RetryPolicy{MaxRetries: 2, InitialBackoff: time.Nanosecond}})
	if err != nil || attempts != 2 || writes != 2 || after != 2 || result.Items[0].ID != "1" || result.Items[1].ID != "2" {
		t.Fatal(result, err, attempts, writes, after)
	}
	attempts = 0
	r.client.transport = TransportFunc(func(*http.Request) (*http.Response, error) { attempts++; return nil, io.ErrUnexpectedEOF })
	if _, err = r.BulkWithOptions(context.Background(), ops, BulkOptions{Retry: RetryPolicy{MaxRetries: 2}}); !errors.Is(err, io.ErrUnexpectedEOF) || attempts != 1 {
		t.Fatal(err, attempts)
	}
	r.client.transport = TransportFunc(func(*http.Request) (*http.Response, error) {
		return response(200, `{"items":[{"index":{"status":201}}]}`), nil
	})
	r.hooks.AfterWrite = func(context.Context, Operation, WriteResult) error { return &Error{Status: 429} }
	result, err = r.BulkWithOptions(context.Background(), ops[:1], BulkOptions{Retry: RetryPolicy{MaxRetries: 1}})
	var committed *CommittedError
	if err == nil || !errors.As(result.Items[0].Err, &committed) {
		t.Fatal(result, err)
	}
}
func TestBulkSequences(t *testing.T) {
	r := testRepo(t, func(req *http.Request) (*http.Response, error) {
		return response(200, `{"items":[{"create":{"_id":"generated","status":201}}]}`), nil
	})
	produced, callbacks := 0, 0
	seq := func(yield func(BulkOperation[testDoc]) bool) {
		for range 5 {
			produced++
			if !yield(BulkOperation[testDoc]{Action: BulkCreate}) {
				return
			}
		}
	}
	err := r.BulkSeq(context.Background(), seq, BulkStreamOptions{BatchSize: 1}, func(result BulkBatchResult) error {
		callbacks++
		if result.Err != nil {
			t.Fatal(result.Err)
		}
		return io.EOF
	})
	if !errors.Is(err, io.EOF) || produced != 1 || callbacks != 1 {
		t.Fatal(err, produced, callbacks)
	}
	fallible := func(yield func(BulkOperation[testDoc], error) bool) {
		if yield(BulkOperation[testDoc]{Action: BulkCreate}, nil) {
			yield(BulkOperation[testDoc]{}, io.ErrUnexpectedEOF)
		}
	}
	err = r.BulkSeq2(context.Background(), fallible, BulkStreamOptions{}, func(result BulkBatchResult) error {
		if result.Result.Items[0].ID != "generated" {
			t.Fatal(result)
		}
		return nil
	})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	for _, o := range []BulkStreamOptions{{Workers: 65}, {FlushInterval: -time.Second}, {BatchSize: -1}, {Retry: RetryPolicy{MaxRetries: -1}}} {
		if err = r.BulkSeq(context.Background(), seq, o, func(BulkBatchResult) error { return nil }); !errors.Is(err, ErrValidation) {
			t.Fatal(err)
		}
	}
	if err = r.BulkSeq(context.Background(), nil, BulkStreamOptions{}, func(BulkBatchResult) error { return nil }); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
}

func TestOfficialOperationWrappers(t *testing.T) {
	r := testRepo(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("retry_on_conflict") != "3" || !strings.Contains(req.URL.EscapedPath(), "x%2Fy") {
			t.Fatal(req.URL)
		}
		return response(200, `{"_id":"x/y","result":"updated","get":{"_source":{"name":"after"}}}`), nil
	})
	req := &update.Request{Script: &types.Script{Source: "ctx._source.name = 'after'"}, Source_: true}
	out, err := r.UpdateWith(context.Background(), "x/y", req, UpdateOptions{RetryOnConflict: 3})
	if err != nil || out.Get.Source.Name != "after" {
		t.Fatal(out, err)
	}
	for _, invalid := range []*update.Request{nil, {}, {Doc: json.RawMessage(`{}`), Script: &types.Script{}}, {Doc: json.RawMessage(`null`)}, {Doc: json.RawMessage(`[]`)}} {
		if _, err = r.UpdateWith(context.Background(), "x", invalid, UpdateOptions{}); err == nil {
			t.Fatal(invalid)
		}
	}
	query := &types.Query{MatchAll: &types.MatchAllQuery{}}
	r.client.transport = TransportFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("wait_for_completion") != "false" || req.Header.Get("Accept") != "application/json" {
			t.Fatal(req.URL, req.Header)
		}
		return response(200, `{"task":"node:1"}`), nil
	})
	u, err := r.UpdateByQuery(context.Background(), &updatebyquery.Request{Query: query}, ByQueryOptions{Async: true, Routing: []string{"r"}, Conflicts: conflicts.Proceed})
	if err != nil || *u.Task != "node:1" {
		t.Fatal(u, err)
	}
	d, err := r.DeleteByQuery(context.Background(), &deletebyquery.Request{Query: query}, ByQueryOptions{Async: true})
	if err != nil || *d.Task != "node:1" {
		t.Fatal(d, err)
	}
	r.client.transport = TransportFunc(func(*http.Request) (*http.Response, error) { return response(200, `{"timed_out":true}`), nil })
	if _, err = r.UpdateByQuery(context.Background(), &updatebyquery.Request{Query: query}, ByQueryOptions{}); err == nil {
		t.Fatal("partial update accepted")
	}
	if _, err = r.DeleteByQuery(context.Background(), &deletebyquery.Request{Query: query}, ByQueryOptions{}); err == nil {
		t.Fatal("partial delete accepted")
	}
}

func TestFacetsAndGeo(t *testing.T) {
	facets := map[string]Facet{"category": {Aggregation: TermsAgg("category", 10, nil), Selection: Term("category", "books")}, "color": {Aggregation: TermsAgg("color", 10, nil), Selection: Term("color", "red")}}
	search := WithFacets(NewSearch(MatchAll()), facets)
	raw, err := json.Marshal(search)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Post json.RawMessage `json:"post_filter"`
		Aggs map[string]struct {
			Filter json.RawMessage `json:"filter"`
		} `json:"aggs"`
	}
	_ = json.Unmarshal(raw, &body)
	if bytes.Contains(body.Aggs["category"].Filter, []byte("books")) || !bytes.Contains(body.Aggs["category"].Filter, []byte("red")) || !bytes.Contains(body.Post, []byte("books")) {
		t.Fatal(string(raw))
	}
	facets["category"] = Facet{}
	again, _ := json.Marshal(search)
	if string(again) != string(raw) {
		t.Fatal("facet snapshot mutated")
	}
	var aggs map[string]json.RawMessage
	_ = json.Unmarshal([]byte(`{"filter#category":{"sterms#values":{"buckets":[{"key":"books","doc_count":2}]}}}`), &aggs)
	values, err := DecodeFacet[struct{ Buckets []struct{ Key string } }](aggs, "category")
	if err != nil || values.Buckets[0].Key != "books" {
		t.Fatal(values, err)
	}
	point := GeoPoint{Lat: 41, Lon: 29}
	schema, err := NewSchema[struct {
		Point GeoPoint `json:"point"`
	}]("geo")
	if err != nil || !strings.Contains(string(schema.Mapping()), "geo_point") {
		t.Fatal(schema, err)
	}
	for _, q := range []Query{Geo("point").WithinDistance("5km", point), Percolate(types.PercolateQuery{Field: "query", Document: json.RawMessage(`{"name":"go"}`)}), Object("comments").Nested(ChildField[string](Object("comments"), "name").Eq("go"))} {
		if _, err := json.Marshal(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []Query{Geo("point").WithinDistance("", point), Percolate(types.PercolateQuery{Field: "query"})} {
		if q.Err() == nil {
			t.Fatal(q)
		}
	}
}

func TestUpdateWithPatchPolicies(t *testing.T) {
	seen := 0
	r := testRepo(t, func(req *http.Request) (*http.Response, error) {
		seen++
		body := readBody(t, req)
		if string(body["doc"]) != `{"age":9007199254740993}` {
			t.Fatal(body)
		}
		return response(200, `{"result":"updated"}`), nil
	})
	yes := true
	req := &update.Request{Doc: json.RawMessage(`{"age":9007199254740993}`), DocAsUpsert: &yes}
	if _, err := r.UpdateWith(context.Background(), "1", req, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if req.DocAsUpsert == nil || len(req.Upsert) != 0 {
		t.Fatal("request mutated")
	}
	for _, bad := range []*update.Request{{Doc: json.RawMessage(`{} {}`)}, {Doc: json.RawMessage(`{`)}, {Doc: json.RawMessage(`{}`), DocAsUpsert: &yes, Upsert: json.RawMessage(`{}`)}, {Doc: json.RawMessage(`{}`), Upsert: json.RawMessage(`[]`)}} {
		if _, err := r.UpdateWith(context.Background(), "1", bad, UpdateOptions{}); err == nil {
			t.Fatal(bad)
		}
	}
	seq, term := int64(1), int64(1)
	for _, o := range []UpdateOptions{{RetryOnConflict: -1}, {RetryOnConflict: 1, WriteOptions: WriteOptions{IfSeqNo: &seq, IfPrimaryTerm: &term}}, {WriteOptions: WriteOptions{IfSeqNo: &seq}}, {WriteOptions: WriteOptions{Pipeline: "p"}}} {
		if _, err := r.UpdateWith(context.Background(), "1", req, o); !errors.Is(err, ErrValidation) {
			t.Fatal(err)
		}
	}
	if _, err := r.UpdateWith(context.Background(), "", req, UpdateOptions{}); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	r.hooks.BeforePatch = func(context.Context, string, Patch) error { return io.EOF }
	if _, err := r.UpdateWith(context.Background(), "1", req, UpdateOptions{}); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	r.hooks.BeforePatch = func(_ context.Context, _ string, p Patch) error { p["bad"] = make(chan int); return nil }
	if _, err := r.UpdateWith(context.Background(), "1", req, UpdateOptions{}); err == nil {
		t.Fatal("invalid patch accepted")
	}
	r.hooks.BeforePatch = nil
	r.hooks.Validate = func(context.Context, *testDoc) error { return io.EOF }
	if _, err := r.UpdateWith(context.Background(), "1", req, UpdateOptions{}); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if seen != 1 {
		t.Fatal(seen)
	}
	r.schema.join = &joinInfo{field: "relation"}
	if _, err := r.UpdateWith(context.Background(), "1", req, UpdateOptions{}); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	if _, err := r.UpdateWith(context.Background(), "1", &update.Request{Doc: json.RawMessage(`{}`), Upsert: json.RawMessage(`{}`)}, UpdateOptions{WriteOptions: WriteOptions{Routing: "r"}}); err == nil {
		t.Fatal("invalid join upsert accepted")
	}
}

func TestIndexerPreparationAndMetadata(t *testing.T) {
	c := testClient(func(req *http.Request) (*http.Response, error) {
		return response(200, `{"_id":"server-id","_index":"features","found":true,"_source":{"name":"Ada"}}`), nil
	})
	schema, _ := NewSchemaFor[featureModel]()
	r, _ := NewRepository(c, schema)
	hit, err := r.Get(context.Background(), "server-id", "tenant")
	if err != nil || hit.Source.DocumentID() != "server-id" || hit.Source.DocumentRouting() != "tenant" {
		t.Fatal(hit, err)
	}
	op := BulkOperation[featureModel]{Action: BulkCreate, Document: featureModel{DocumentMeta: DocumentMeta{Routing: "tenant"}}}
	prepared, err := r.PrepareIndexerOperation(context.Background(), op)
	if err != nil || prepared.ID != "" || prepared.Routing != "tenant" || !bytes.Contains(prepared.Body, []byte("created_at")) {
		t.Fatal(prepared, err)
	}
	item := prepared.Complete(context.Background(), WriteResult{Metadata: Metadata{ID: "generated"}}, 201, nil)
	if item.Err != nil || item.Routing != "tenant" {
		t.Fatal(item)
	}
	r.hooks.AfterWrite = func(context.Context, Operation, WriteResult) error { return io.EOF }
	item = prepared.Complete(context.Background(), WriteResult{}, 201, nil)
	var committed *CommittedError
	if !errors.As(item.Err, &committed) {
		t.Fatal(item)
	}
	item = prepared.Complete(context.Background(), WriteResult{}, 429, nil)
	if !errors.Is(item.Err, ErrTooManyRequests) {
		t.Fatal(item)
	}
	for _, o := range []WriteOptions{{Pipeline: "p"}, {Refresh: "true"}} {
		op.Options = o
		if _, err := r.PrepareIndexerOperation(context.Background(), op); !errors.Is(err, ErrUnsupported) {
			t.Fatal(err)
		}
	}
	op.Options = WriteOptions{}
	op.Action = BulkIndex
	if _, err := r.PrepareIndexerOperation(context.Background(), op); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	op.Document.ID = "from-model"
	prepared, err = r.PrepareIndexerOperation(context.Background(), op)
	if err != nil || prepared.ID != "from-model" {
		t.Fatal(prepared, err)
	}
	op.Action = "bad"
	if _, err := r.PrepareIndexerOperation(context.Background(), op); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
}

func TestFacetValidation(t *testing.T) {
	facet := map[string]Facet{"f": {Aggregation: TermsAgg("x", 10, nil)}}
	for _, base := range []Search{{err: io.EOF}, Search{}.With("post_filter", MatchAll()), Search{}.With("aggs", []int{1}), Search{}.With("aggregations", []int{1}), Search{}.With("aggs", map[string]any{"f": map[string]any{}}), Search{}.With("aggs", map[string]any{"a": 1}).With("aggregations", map[string]any{"b": 1})} {
		if WithFacets(base, facet).Err() == nil {
			t.Fatal(base)
		}
	}
	for _, f := range []map[string]Facet{{"": {Aggregation: TermsAgg("x", 10, nil)}}, {"f": {}}, {"f": {Aggregation: TermsAgg("x", 10, nil), Selection: Query{err: io.EOF}}}} {
		if WithFacets(Search{}, f).Err() == nil {
			t.Fatal(f)
		}
	}
	if WithFacets(Search{}, nil).Err() != nil {
		t.Fatal("empty facets")
	}
	if WithFacets(Search{}.With("aggregations", map[string]any{"existing": map[string]any{"avg": map[string]string{"field": "price"}}}), facet).Err() != nil {
		t.Fatal("existing aggregation")
	}
	if WithFacets(Search{}.With("aggs", nil), facet).Err() != nil {
		t.Fatal("nil aggregations")
	}
	if _, err := DecodeFacet[any](nil, "missing"); err == nil {
		t.Fatal("missing facet")
	}
	_ = (&IncompleteOperationError{Operation: "test"}).Error()
}

func TestRepositorySequencesClose(t *testing.T) {
	closed := 0
	r := testRepo(t, func(req *http.Request) (*http.Response, error) {
		if req.Method == "DELETE" {
			closed++
			return response(200, `{}`), nil
		}
		if strings.HasSuffix(req.URL.Path, "/_pit") {
			return response(200, `{"id":"pit"}`), nil
		}
		return response(200, `{"hits":{"hits":[{"_source":{"name":"Ada"},"sort":[1]}]}}`), nil
	})
	for source, err := range r.Sources(context.Background(), NewSearch(MatchAll()), 1, "1m") {
		if err != nil || source.Name != "Ada" {
			t.Fatal(source, err)
		}
		break
	}
	if closed != 1 {
		t.Fatal(closed)
	}
	for _, err := range r.Each(context.Background(), Search{}, 0, "") {
		if !errors.Is(err, ErrValidation) {
			t.Fatal(err)
		}
	}
}

func TestStoredRoutingHydration(t *testing.T) {
	schema, _ := NewSchemaFor[featureModel]()
	r, _ := NewRepository(testClient(nil), schema)
	hit := Hit[featureModel]{Metadata: Metadata{ID: "1"}, Fields: map[string]json.RawMessage{"_routing": json.RawMessage(`["tenant"]`)}}
	if err := r.afterRead(context.Background(), &hit); err != nil || hit.Source.DocumentRouting() != "tenant" || hit.Source.DocumentID() != "1" {
		t.Fatal(hit, err)
	}
	hit.Routing = ""
	hit.Fields["_routing"] = json.RawMessage(`[]`)
	if err := r.afterRead(context.Background(), &hit); err == nil {
		t.Fatal("ambiguous routing")
	}
}
