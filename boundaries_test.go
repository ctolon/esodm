package esodm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBulkValidationAndCancellation(t *testing.T) {
	ctx := context.Background()
	r := testRepo(t, func(*http.Request) (*http.Response, error) { return response(503, `{"error":"unavailable"}`), nil })
	seq, term := int64(1), int64(1)
	invalid := []BulkOperation[testDoc]{{Action: "bad", ID: "1"}, {Action: BulkIndex}, {Action: BulkIndex, ID: "1", Options: WriteOptions{Refresh: "true"}}, {Action: BulkIndex, ID: "1", Options: WriteOptions{IfSeqNo: &seq}}, {Action: BulkCreate, ID: "1", Options: WriteOptions{IfSeqNo: &seq, IfPrimaryTerm: &term}}, {Action: BulkUpdate, ID: "1", Options: WriteOptions{Pipeline: "pipeline"}}}
	for _, op := range invalid {
		if _, err := r.Bulk(ctx, []BulkOperation[testDoc]{op}, ""); !errors.Is(err, ErrValidation) {
			t.Fatal(op, err)
		}
	}
	if _, err := r.Bulk(ctx, make([]BulkOperation[testDoc], 10001), ""); err == nil {
		t.Fatal("oversized batch")
	}
	if _, err := r.Bulk(ctx, nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Bulk(ctx, []BulkOperation[testDoc]{{Action: BulkDelete, ID: "1"}}, "bad"); err == nil {
		t.Fatal("refresh")
	}
	if _, err := r.Bulk(ctx, []BulkOperation[testDoc]{{Action: BulkDelete, ID: "1"}}, ""); err == nil {
		t.Fatal("transport error")
	}
	r.hooks.BeforeWrite = func(context.Context, Operation, *testDoc) error { return io.ErrUnexpectedEOF }
	if _, err := r.captureBulk(ctx, []BulkOperation[testDoc]{{Action: BulkIndex, ID: "1"}}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	r.hooks.BeforeWrite = nil
	r.hooks.Validate = func(context.Context, *testDoc) error { return io.ErrUnexpectedEOF }
	if _, err := r.captureBulk(ctx, []BulkOperation[testDoc]{{Action: BulkIndex, ID: "1"}}); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	r.hooks.Validate = nil
	body, err := r.captureBulk(ctx, []BulkOperation[testDoc]{{Action: BulkIndex, ID: "1", Options: WriteOptions{Routing: "r", Pipeline: "p", IfSeqNo: &seq, IfPrimaryTerm: &term}}, {Action: BulkUpdate, ID: "2", Patch: Patch{"age": 0}}})
	if err != nil || !strings.Contains(string(body), `"doc":{"age":0}`) || !strings.Contains(string(body), `"if_primary_term":1`) {
		t.Fatal(string(body), err)
	}
	if _, err := r.captureBulk(ctx, []BulkOperation[testDoc]{{Action: BulkUpdate, ID: "2", Patch: Patch{"x": make(chan int)}}}); err == nil {
		t.Fatal("unserializable patch")
	}
	for _, raw := range []string{`{"items":[]}`, `{"items":[{"wrong":{"status":200}}]}`, `{"items":[{"delete":{"status":404}}]}`} {
		r.client.transport = transportFunc(func(*http.Request) (*http.Response, error) { return response(200, raw), nil })
		if _, err := r.Bulk(ctx, []BulkOperation[testDoc]{{Action: BulkDelete, ID: "1"}}, ""); err == nil {
			t.Fatal(raw)
		}
	}
	_ = (&BulkError{1}).Error()
	input := make(chan BulkOperation[testDoc])
	if err := r.BulkStream(ctx, input, BulkStreamOptions{Workers: -1}, func(BulkBatchResult) error { return nil }); err == nil {
		t.Fatal("options")
	}
	if err := r.BulkStream(ctx, input, BulkStreamOptions{Refresh: "bad"}, func(BulkBatchResult) error { return nil }); err == nil {
		t.Fatal("refresh")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := r.BulkStream(canceled, input, BulkStreamOptions{}, func(BulkBatchResult) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	buffer := make(chan BulkOperation[testDoc], 1)
	buffer <- BulkOperation[testDoc]{Action: BulkDelete, ID: "1"}
	close(buffer)
	if err := r.BulkStream(ctx, buffer, BulkStreamOptions{}, func(BulkBatchResult) error { return io.EOF }); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}
func TestRepositoryInvalidInputsAndHooks(t *testing.T) {
	ctx := context.Background()
	r := testRepo(t, func(*http.Request) (*http.Response, error) { return response(200, `{"found":false}`), nil })
	seq := int64(1)
	bad := WriteOptions{Refresh: "bad"}
	checks := []func() error{
		func() error { _, e := r.Get(ctx, "", ""); return e }, func() error { _, e := r.Exists(ctx, "", ""); return e }, func() error { _, e := r.Create(ctx, "", testDoc{}, WriteOptions{}); return e },
		func() error { _, e := r.Create(ctx, "1", testDoc{}, bad); return e }, func() error { _, e := r.Update(ctx, "", nil, WriteOptions{}); return e }, func() error { _, e := r.Update(ctx, "1", nil, bad); return e },
		func() error { _, e := r.Delete(ctx, "", WriteOptions{}); return e }, func() error { _, e := r.Delete(ctx, "1", bad); return e }, func() error { _, e := r.Update(ctx, "1", nil, WriteOptions{Pipeline: "p"}); return e }, func() error { _, e := r.Delete(ctx, "1", WriteOptions{Pipeline: "p"}); return e },
		func() error {
			_, e := r.Create(ctx, "1", testDoc{}, WriteOptions{IfSeqNo: &seq, IfPrimaryTerm: &seq})
			return e
		},
	}
	for _, check := range checks {
		if !errors.Is(check(), ErrValidation) {
			t.Fatal("accepted invalid input")
		}
	}
	if _, err := r.Get(ctx, "1", ""); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	r.hooks.BeforeWrite = func(context.Context, Operation, *testDoc) error { return io.EOF }
	if _, err := r.Replace(ctx, "1", testDoc{}, WriteOptions{}); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	r.hooks.Validate = func(context.Context, *testDoc) error { return io.EOF }
	if _, err := r.Upsert(ctx, "1", nil, testDoc{}, WriteOptions{}); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	if _, err := NewRepository[testDoc](nil, r.Schema()); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	if _, err := NewRepository(r.client, r.Schema(), Hooks[testDoc]{}); err != nil {
		t.Fatal(err)
	}
	if r.client.Transport() == nil {
		t.Fatal("transport")
	}
	r.client.transport = transportFunc(func(*http.Request) (*http.Response, error) { return response(503, `{}`), nil })
	if err := r.EnsureIndex(ctx); err == nil {
		t.Fatal("masked error")
	}
}
func TestReferenceFailuresAndBatching(t *testing.T) {
	ctx := context.Background()
	c := testClient(func(*http.Request) (*http.Response, error) {
		return response(200, `{"docs":[{"found":false,"status":500,"error":{"type":"shard_failure","reason":"offline"}}]}`), nil
	})
	ref := Ref[testDoc]{Index: "people", ID: "1"}
	result, err := Relation[testDoc]("related").Load(ctx, c, ref)
	if err != nil || result[0].Err == nil || result[0].Found {
		t.Fatal(result, err)
	}
	if Relation[testDoc]("related").Name() != "related" {
		t.Fatal("field name")
	}
	if _, err := Load[testDoc](ctx, nil, nil); err == nil {
		t.Fatal("nil client")
	}
	if _, err := Load(ctx, c, []Ref[testDoc]{{ID: "1"}}); err == nil {
		t.Fatal("empty index")
	}
	if _, err := Load(ctx, c, []Ref[testDoc]{{Index: "p"}}); err == nil {
		t.Fatal("empty ID")
	}
	c.transport = transportFunc(func(*http.Request) (*http.Response, error) { return response(200, `{"docs":[]}`), nil })
	if _, err := Load(ctx, c, []Ref[testDoc]{ref}); err == nil {
		t.Fatal("missing response")
	}
	c.transport = transportFunc(func(*http.Request) (*http.Response, error) { return nil, io.EOF })
	if _, err := Load(ctx, c, []Ref[testDoc]{ref}); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if _, err := Preload[testDoc](ctx, c, nil, nil, PreloadOptions{}); err == nil {
		t.Fatal("nil children")
	}
	if _, err := Preload(ctx, c, []Ref[testDoc]{ref, {Index: "people", ID: "2"}}, func(testDoc) []Ref[testDoc] { return nil }, PreloadOptions{MaxDocuments: 1}); err == nil {
		t.Fatal("document limit")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Preload(canceled, c, []Ref[testDoc]{ref}, func(testDoc) []Ref[testDoc] { return nil }, PreloadOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	r := testRepo(t, func(*http.Request) (*http.Response, error) {
		return response(200, `{"docs":[{"found":true,"_source":{"name":"Ada"}}]}`), nil
	})
	r.hooks.AfterRead = func(_ context.Context, h *Hit[testDoc]) error { h.Source.Active = true; return nil }
	got, err := r.MGet(ctx, []string{"1"}, "")
	if err != nil || !got[0].Hit.Source.Active {
		t.Fatal(got, err)
	}
}
func TestIteratorFailures(t *testing.T) {
	ctx := context.Background()
	r := testRepo(t, func(req *http.Request) (*http.Response, error) {
		if req.Method == "POST" && strings.HasSuffix(req.URL.Path, "/_pit") {
			return response(200, `{"id":"pit"}`), nil
		}
		if req.Method == "DELETE" {
			return response(404, `{}`), nil
		}
		return response(200, `{"hits":{"hits":[{"_id":"1","sort":[1]}]}}`), nil
	})
	for _, size := range []int{0, 10001} {
		if _, err := r.Iterate(ctx, Search{}, size, ""); err == nil {
			t.Fatal("page size")
		}
	}
	for _, duration := range []string{"invalid", "-1s", "0s"} {
		if _, err := r.Iterate(ctx, Search{}, 1, duration); err == nil {
			t.Fatal(duration)
		}
	}
	for _, search := range []Search{Search{}.From(1), Search{}.PIT("x", "1m"), Search{}.Size(-1)} {
		if _, err := r.Iterate(ctx, search, 1, ""); err == nil {
			t.Fatal("invalid search")
		}
	}
	it, err := r.Iterate(ctx, Search{}, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if !it.Next() || it.Next() || it.Err() == nil {
		t.Fatal("non advancing iterator", it.Err())
	}
	r.client.transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/_pit") {
			return response(200, `{"id":"pit"}`), nil
		}
		return response(200, `{"hits":{"hits":[{"_id":"1"}]}}`), nil
	})
	it, err = r.Iterate(ctx, Search{}, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if it.Next() || it.Err() == nil {
		t.Fatal("missing sort")
	}
	r.client.transport = transportFunc(func(*http.Request) (*http.Response, error) { return response(200, `{}`), nil })
	if _, err := r.Iterate(ctx, Search{}, 1, ""); err == nil {
		t.Fatal("empty PIT")
	}
	r.client.transport = transportFunc(func(*http.Request) (*http.Response, error) { return nil, io.EOF })
	if _, err := r.Iterate(ctx, Search{}, 1, ""); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}
func TestVectorSourceAndSchemaBoundaries(t *testing.T) {
	type vectorDoc struct {
		V []float32 `json:"v" es:"type=dense_vector"`
	}
	s, err := NewSchema[vectorDoc]("vectors", SchemaConfig{Properties: map[string]FieldMapping{"v": {Type: "dense_vector", Dims: 3}}})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := NewRepository(testClient(nil), s)
	for _, source := range []any{nil, true, map[string]any{"includes": []string{"v"}}, map[string]any{"exclude_vectors": true}, false, []string{"v"}} {
		search := Search{}
		if source != nil {
			search = search.With("_source", source)
		}
		out := r.includeSourceVectors(search)
		b, err := json.Marshal(out)
		if err != nil || !json.Valid(b) {
			t.Fatal(string(b), err)
		}
	}
	for _, props := range []map[string]FieldMapping{{"x": {}}, {"x": {Type: "dense_vector", Dims: 0}}, {"x": {Type: "join"}}, {"x": {Type: "join", Relations: map[string]any{"p": "c"}}, "y": {Type: "join", Relations: map[string]any{"p": "c"}}}} {
		if err := validateProperties(props, 0); err == nil {
			t.Fatal(props)
		}
	}
	if err := validateProperties(nil, 33); err == nil {
		t.Fatal("depth")
	}
	type binary struct {
		Data []byte `json:"data"`
	}
	b, err := NewSchema[binary]("binary")
	if err != nil || !strings.Contains(string(b.mapping), `"binary"`) {
		t.Fatal(b, err)
	}
	type skipped struct {
		Field string `json:"-" es:"-"`
	}
	if _, err := NewSchema[skipped]("x"); err != nil {
		t.Fatal(err)
	}
	type unsupported struct{ F func() }
	if _, err := NewSchema[unsupported]("x"); err == nil {
		t.Fatal("function")
	}
	type child struct{ A string }
	type embedded struct{ child }
	if _, err := NewSchema[embedded]("x"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSchema[testDoc]("x", nil); err == nil {
		t.Fatal("multiple configs")
	}
	for _, dynamic := range []string{"true", "false", "runtime"} {
		if _, err := NewSchema[testDoc]("x", SchemaConfig{Dynamic: Dynamic(dynamic)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewSchema[testDoc]("x", SchemaConfig{Settings: map[string]any{"bad": make(chan int)}}); err == nil {
		t.Fatal("settings encoding")
	}
	for _, agg := range []Aggregation{{}, Agg("", nil, nil), Agg("sum", make(chan int), nil)} {
		if _, err := json.Marshal(agg); err == nil {
			t.Fatal("bad aggregation")
		}
	}
	if _, err := DecodeAggregation[int](SearchResult[json.RawMessage]{}, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := json.Marshal(Search{}.With("", 1)); err == nil {
		t.Fatal("empty option")
	}
	if _, err := json.Marshal(Search{}.With("x", make(chan int)).Size(1)); err == nil {
		t.Fatal("lost error")
	}
	if _, err := json.Marshal(Search{}.HybridRRF(MatchAll(), KNN{}, 0, 0)); err == nil {
		t.Fatal("RRF")
	}
	if _, err := json.Marshal(Search{}.From(1)); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentIDAndEmptyQueries(t *testing.T) {
	for _, id := range []string{"slash/id", "spaces are legal", "雪", ".", ".."} {
		if err := validateID(id); err != nil {
			t.Fatal(id, err)
		}
	}
	for _, id := range []string{"", strings.Repeat("x", 513), "\xff"} {
		if err := validateID(id); err == nil {
			t.Fatal("invalid ID")
		}
	}
	for _, q := range []Query{Or(), IDs(), Terms[string]("x")} {
		b, err := json.Marshal(q)
		if err != nil || string(b) != `{"match_none":{}}` {
			t.Fatal(string(b), err)
		}
	}
}

// captureBulk asserts the actual public Bulk wire path without shipping a second encoder.
// The transport stops after capturing the request, before any completion hooks.
func (r *Repository[T]) captureBulk(ctx context.Context, ops []BulkOperation[T]) ([]byte, error) {
	captured := errors.New("request captured")
	var body []byte
	client := *r.client
	client.transport = TransportFunc(func(request *http.Request) (*http.Response, error) {
		defer request.Body.Close()
		var err error
		body, err = io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		return nil, captured
	})
	repo := *r
	repo.client = &client
	_, err := repo.Bulk(ctx, ops, "")
	if errors.Is(err, captured) {
		err = nil
	}
	return body, err
}
