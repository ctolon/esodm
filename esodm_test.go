package esodm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) Perform(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}
func testClient(f transportFunc) *Client {
	return &Client{transport: f, version: Version{9, 4, 5}, config: Config{MaxResponseBytes: 1 << 20}}
}

type testDoc struct {
	Name     string  `json:"name" es:"type=text"`
	Age      int     `json:"age"`
	Active   bool    `json:"active"`
	Optional *string `json:"optional,omitempty"`
}

func testRepo(t *testing.T, f transportFunc) *Repository[testDoc] {
	t.Helper()
	s, err := NewSchema[testDoc]("people")
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRepository(testClient(f), s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func readBody(t *testing.T, r *http.Request) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestConnectAndErrors(t *testing.T) {
	for _, version := range []string{"8.18.1", "8.19.7", "9.4.5", "9.5.0"} {
		t.Run(version, func(t *testing.T) {
			c, err := Connect(context.Background(), transportFunc(func(r *http.Request) (*http.Response, error) {
				return response(200, `{"version":{"number":"`+version+`"}}`), nil
			}), Config{})
			if err != nil || c.Version().String() != version {
				t.Fatalf("%v %v", c, err)
			}
		})
	}
	for _, version := range []string{"8.18.0", "9.4.4", "10.0.0", "bad"} {
		_, err := Connect(context.Background(), transportFunc(func(r *http.Request) (*http.Response, error) {
			return response(200, `{"version":{"number":"`+version+`"}}`), nil
		}), Config{})
		if err == nil {
			t.Fatal(version)
		}
	}
	if _, err := Connect(context.Background(), nil, Config{}); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	for _, limit := range []int64{-1, 1 << 41} {
		if _, err := Connect(context.Background(), transportFunc(nil), Config{MaxResponseBytes: limit}); !errors.Is(err, ErrValidation) {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		status int
		body   string
		target error
	}{{404, `{"error":{"type":"missing","reason":"gone"}}`, ErrNotFound}, {409, `{"error":"conflict"}`, ErrConflict}, {400, `not json`, ErrValidation}, {503, `{}`, nil}} {
		c := testClient(func(r *http.Request) (*http.Response, error) { return response(tc.status, tc.body), nil })
		err := c.Do(context.Background(), "GET", "/", nil, nil, nil)
		var e *Error
		if !errors.As(err, &e) || e.Status != tc.status || tc.target != nil && !errors.Is(err, tc.target) {
			t.Fatal(err)
		}
		_ = e.Error()
	}
}

type closeBody struct {
	io.Reader
	closed *bool
}

func (b closeBody) Close() error { *b.closed = true; return nil }
func TestTransportBoundaries(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		body  string
		limit int64
		want  error
	}{{`{"ok":true}`, 5, ErrResponseTooLarge}, {`{} {}`, 100, nil}, {``, 100, nil}, {`{`, 100, nil}} {
		closed := false
		c := testClient(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: closeBody{strings.NewReader(tc.body), &closed}}, nil
		})
		c.config.MaxResponseBytes = tc.limit
		var out any
		err := c.Do(ctx, "GET", "/", nil, nil, &out)
		if err == nil || tc.want != nil && !errors.Is(err, tc.want) || !closed {
			t.Fatalf("%v closed=%v", err, closed)
		}
	}
	c := testClient(func(r *http.Request) (*http.Response, error) { return response(200, `{"n":9007199254740993}`), nil })
	var out map[string]any
	if err := c.Do(ctx, "GET", "/", nil, nil, &out); err != nil || out["n"].(json.Number).String() != "9007199254740993" {
		t.Fatalf("%v %v", out, err)
	}
	for _, path := range []string{"https://example.org", "//example.org", "/x?y", "/x#y"} {
		if err := c.Do(ctx, "GET", path, nil, nil, nil); !errors.Is(err, ErrValidation) {
			t.Fatal(path, err)
		}
	}
	//lint:ignore SA1012 Exercise the documented nil-context validation path.
	if err := c.Do(nil, "GET", "/", nil, nil, nil); !errors.Is(err, ErrValidation) { //nolint:staticcheck // Verify explicit rejection of a nil context.
		t.Fatal(err)
	}
	if err := c.Do(ctx, "POST", "/", nil, make(chan int), nil); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	c = testClient(func(r *http.Request) (*http.Response, error) { return nil, nil })
	if c.Do(ctx, "GET", "/", nil, nil, nil) == nil {
		t.Fatal("nil response accepted")
	}
	c = testClient(func(r *http.Request) (*http.Response, error) { return nil, io.ErrUnexpectedEOF })
	if !errors.Is(c.Do(ctx, "GET", "/", nil, nil, nil), io.ErrUnexpectedEOF) {
		t.Fatal("transport error lost")
	}
	var event Event
	c = testClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("routing") != "a/b" {
			t.Fatal(r.URL)
		}
		return response(200, `{}`), nil
	})
	c.config.Observer = func(_ context.Context, e Event) { event = e }
	if err := c.Do(ctx, "GET", "/", url.Values{"routing": {"a/b"}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if event.Method != "GET" || event.Status != 200 || event.Duration < 0 {
		t.Fatal(event)
	}
}

func TestSchema(t *testing.T) {
	type child struct {
		Value string `json:"value"`
	}
	type doc struct {
		Title    string    `json:"title" es:"type=text,analyzer=standard"`
		When     time.Time `json:"when"`
		Children []child   `json:"children" es:"type=nested"`
		Secret   string    `json:"-"`
		Unsigned uint64    `json:"unsigned"`
		Value    float64   `json:"value"`
	}
	s, err := NewSchema[doc]("docs", SchemaConfig{Settings: map[string]any{"number_of_shards": 1}})
	if err != nil {
		t.Fatal(err)
	}
	var mapping struct {
		Properties map[string]FieldMapping `json:"properties"`
	}
	if err = json.Unmarshal(s.Mapping(), &mapping); err != nil {
		t.Fatal(err)
	}
	if mapping.Properties["when"].Type != "date" || mapping.Properties["children"].Type != "nested" || len(mapping.Properties) != 5 {
		t.Fatal(mapping)
	}
	copy := s.Mapping()
	copy[0] = 'x'
	if s.Mapping()[0] != '{' {
		t.Fatal("mutable schema")
	}
	if s.Index() != "docs" || len(s.Settings()) == 0 {
		t.Fatal(s)
	}
	if _, err = NewSchema[doc]("docs", SchemaConfig{Properties: map[string]FieldMapping{"title": {Type: "keyword"}}}); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	if _, err = NewSchema[doc]("docs", SchemaConfig{Properties: map[string]FieldMapping{"missing": {Type: "text"}}}); err == nil {
		t.Fatal("unknown field")
	}
	if _, err = NewSchema[string]("docs"); err == nil {
		t.Fatal("nonstruct")
	}
	if _, err = NewSchema[doc]("*"); err == nil {
		t.Fatal("wildcard")
	}
	if _, err = NewSchema[doc]("docs", SchemaConfig{Dynamic: "nonsense"}); err == nil {
		t.Fatal("dynamic")
	}
	type recursive struct {
		Next *recursive `json:"next"`
	}
	if _, err := NewSchema[recursive]("docs"); err == nil {
		t.Fatal("cycle")
	}
	type invalid struct {
		Data map[string]any `json:"data"`
	}
	if _, err := NewSchema[invalid]("docs"); err == nil {
		t.Fatal("map needs type")
	}
	type ignored struct {
		Secret string `es:"-"`
	}
	if _, err := NewSchema[ignored]("docs"); err == nil {
		t.Fatal("source mismatch")
	}
	duplicate := reflect.StructOf([]reflect.StructField{
		{Name: "A", Type: reflect.TypeFor[string](), Tag: reflect.StructTag(`json:"x"`)},
		{Name: "B", Type: reflect.TypeFor[string](), Tag: reflect.StructTag(`json:"x"`)},
	})
	if _, _, err := inferProperties(duplicate, map[reflect.Type]bool{}, 0); err == nil {
		t.Fatal("duplicate")
	}
	type vector struct {
		Embedding []float32 `json:"embedding" es:"type=dense_vector"`
	}
	if _, err := NewSchema[vector]("docs", SchemaConfig{Properties: map[string]FieldMapping{"embedding": {Type: "dense_vector", Dims: 3}}}); err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"type", "type=", "type=text,type=keyword", "unknown=x"} {
		if _, _, err := parseTag(tag); err == nil {
			t.Fatal(tag)
		}
	}
}
func TestQueryImmutabilityAndTypes(t *testing.T) {
	values := []string{"a", "b"}
	q := Terms("name", values...)
	values[0] = "changed"
	raw, err := json.Marshal(q)
	if err != nil || strings.Contains(string(raw), "changed") {
		t.Fatal(string(raw), err)
	}
	f := OrderedValue[int]("age")
	text := Text("name")
	queries := []Query{And(f.GTE(18), text.Match("Go")), Or(f.LT(30), f.GT(60)), Not(f.Eq(4)), Filter(f.Between(1, 10)), f.LTE(9), f.In(1, 2), f.Exists(), text.Phrase("Go book"), Prefix("name", "G"), Wildcard("name", "G*"), IDs("1"), MultiMatch("Go", "name"), Nested("children", MatchAll()), HasChild("child", MatchAll()), HasParent("parent", MatchAll()), ParentID("child", "1"), MatchNone(), And()}
	for _, q := range queries {
		b, err := json.Marshal(q)
		if err != nil || !json.Valid(b) {
			t.Fatalf("%s %v", b, err)
		}
	}
	for _, q := range []Query{Term("", 1), Exists(""), RawQuery([]byte(`[]`)), RawQuery([]byte(`{"a":{},"b":{}}`))} {
		if _, err := json.Marshal(q); err == nil {
			t.Fatal("invalid query")
		}
	}
	a := map[string]Aggregation{"ages": TermsAgg("age", 5, map[string]Aggregation{"avg": MetricAgg("avg", "age")}), "pipeline": PipelineAgg("bucket_script", map[string]string{"x": "age"}, "params.x")}
	base := NewSearch(MatchAll()).Size(10)
	changed := base.Size(2).Sort(Sort{Field: "age", Desc: true}).Source([]string{"name"}, []string{}).Highlight("name").Collapse("name").Aggregations(a).Runtime(map[string]any{"day": map[string]any{"type": "keyword"}}).Suggest("names", "name", "g", "term")
	b, _ := json.Marshal(base)
	if !strings.Contains(string(b), `"size":10`) {
		t.Fatal(string(b))
	}
	b, err = json.Marshal(changed)
	if err != nil || !json.Valid(b) {
		t.Fatal(string(b), err)
	}
	for _, s := range []Search{base.Size(-1), base.From(-1), base.Sort(Sort{}), base.KNN(KNN{}), base.HybridRRF(MatchAll(), KNN{}, 1, 1)} {
		if _, err := json.Marshal(s); err == nil {
			t.Fatal("invalid search")
		}
	}
	knn := KNN{Field: "vector", Vector: []float32{1, 2}, K: 2, Candidates: 10}
	if _, err := json.Marshal(base.KNN(knn).HybridRRF(MatchAll(), knn, 10, 60)); err != nil {
		t.Fatal(err)
	}
	if _, err := json.Marshal(SparseVector("sparse", map[string]float32{"go": 1})); err != nil {
		t.Fatal(err)
	}
	var agg struct {
		Value int `json:"value"`
	}
	agg, err = DecodeAgg[struct {
		Value int `json:"value"`
	}](map[string]json.RawMessage{"x": []byte(`{"value":5}`)}, "x")
	if err != nil || agg.Value != 5 {
		t.Fatal(agg, err)
	}
}

func TestRepositoryOperations(t *testing.T) {
	ctx := context.Background()
	var last *http.Request
	var body map[string]json.RawMessage
	r := testRepo(t, func(req *http.Request) (*http.Response, error) {
		last = req
		if req.Body != nil && req.ContentLength != 0 {
			body = readBody(t, req)
		}
		switch {
		case req.Method == "HEAD":
			return response(200, ""), nil
		case strings.HasSuffix(req.URL.Path, "/_count"):
			return response(200, `{"count":7}`), nil
		case req.Method == "GET":
			return response(200, `{"_id":"1","_index":"people","found":true,"_source":{"name":"Ada","age":30}}`), nil
		default:
			return response(200, `{"_id":"1","_index":"people","_seq_no":2,"_primary_term":1,"result":"updated"}`), nil
		}
	})
	var calls []string
	r.hooks = Hooks[testDoc]{BeforeWrite: func(_ context.Context, op Operation, d *testDoc) error {
		calls = append(calls, string(op))
		d.Active = true
		return nil
	}, Validate: func(_ context.Context, d *testDoc) error {
		if d.Name == "bad" {
			return errors.New("bad name")
		}
		return nil
	}, AfterRead: func(_ context.Context, h *Hit[testDoc]) error { h.Source.Active = true; return nil }}
	if err := r.EnsureIndex(ctx); err != nil {
		t.Fatal(err)
	}
	hit, err := r.Get(ctx, "1", "route")
	if err != nil || !hit.Source.Active || hit.Routing != "route" {
		t.Fatal(hit, err)
	}
	if ok, err := r.Exists(ctx, "1", ""); !ok || err != nil {
		t.Fatal(ok, err)
	}
	wr, err := r.Create(ctx, "1", testDoc{Name: "Ada"}, WriteOptions{Refresh: "wait_for"})
	if err != nil || wr.SeqNo == nil || last.URL.Path != "/people/_create/1" || string(body["active"]) != "true" {
		t.Fatal(wr, err, last.URL)
	}
	if _, err := r.Replace(ctx, "1", testDoc{Name: "bad"}, WriteOptions{}); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	seq, term := int64(2), int64(1)
	if _, err := r.Replace(ctx, "1", testDoc{}, WriteOptions{IfSeqNo: &seq, IfPrimaryTerm: &term}); err != nil || last.URL.Query().Get("if_seq_no") != "2" {
		t.Fatal(err)
	}
	if _, err := r.Update(ctx, "1", Patch{"name": nil, "age": 0}, WriteOptions{}); err != nil || string(body["doc"]) != `{"age":0,"name":null}` {
		t.Fatal(string(body["doc"]), err)
	}
	if _, err := r.Upsert(ctx, "1", Patch{"age": 2}, testDoc{Name: "new"}, WriteOptions{}); err != nil || len(body["upsert"]) == 0 {
		t.Fatal(err)
	}
	if _, err := r.Delete(ctx, "1", WriteOptions{}); err != nil || last.Method != "DELETE" {
		t.Fatal(err)
	}
	if count, err := r.Count(ctx, MatchAll()); err != nil || count != 7 {
		t.Fatal(count, err)
	}
	r.hooks.AfterWrite = func(context.Context, Operation, WriteResult) error { return errors.New("hook") }
	_, err = r.Create(ctx, "1", testDoc{}, WriteOptions{})
	var committed *CommittedError
	if !errors.As(err, &committed) || committed.Result.ID != "1" {
		t.Fatal(err)
	}
	_ = committed.Error()
	_ = committed.Unwrap()
	for _, o := range []WriteOptions{{Refresh: "bad"}, {IfSeqNo: &seq}, {IfPrimaryTerm: &term}, {IfSeqNo: new(int64), IfPrimaryTerm: new(int64)}} {
		if _, err := o.values(); !errors.Is(err, ErrValidation) {
			t.Fatal(o, err)
		}
	}
}
func TestMissingAndIndexRace(t *testing.T) {
	ctx := context.Background()
	r := testRepo(t, func(req *http.Request) (*http.Response, error) {
		if req.Method == "PUT" {
			return response(400, `{"error":{"type":"resource_already_exists_exception"}}`), nil
		}
		return response(404, `{"found":false}`), nil
	})
	if err := r.EnsureIndex(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(ctx, "1", ""); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if found, err := r.Exists(ctx, "1", ""); err != nil || found {
		t.Fatal(found, err)
	}
}
func TestLoadAndPreload(t *testing.T) {
	var calls int
	c := testClient(func(req *http.Request) (*http.Response, error) {
		calls++
		m := readBody(t, req)
		var docs []map[string]string
		_ = json.Unmarshal(m["docs"], &docs)
		out := []any{}
		for _, d := range docs {
			out = append(out, map[string]any{"_id": d["_id"], "_index": d["_index"], "found": d["_id"] != "missing", "_source": map[string]any{"name": d["_id"]}})
		}
		b, _ := json.Marshal(map[string]any{"docs": out})
		return response(200, string(b)), nil
	})
	refs := []Ref[testDoc]{{"people", "1", ""}, {"people", "missing", ""}, {"people", "1", ""}}
	loaded, err := Load(context.Background(), c, refs)
	if err != nil || len(loaded) != 3 || loaded[1].Found || loaded[2].Hit.Source.Name != "1" || calls != 1 {
		t.Fatal(loaded, err)
	}
	g, err := Preload(context.Background(), c, refs[:1], func(d testDoc) []Ref[testDoc] { return refs[:1] }, PreloadOptions{})
	if err != nil || len(g.Nodes) != 1 || g.Truncated {
		t.Fatal(g, err)
	}
	g, err = Preload(context.Background(), c, refs[:1], func(d testDoc) []Ref[testDoc] { return []Ref[testDoc]{{"people", d.Name + "x", ""}} }, PreloadOptions{MaxDepth: 2})
	if err != nil || len(g.Nodes) != 2 || !g.Truncated {
		t.Fatal(g, err)
	}
}
func TestJoinValidation(t *testing.T) {
	type doc struct {
		Link Join `json:"link" es:"type=join"`
	}
	s, err := NewSchema[doc]("joins", SchemaConfig{Properties: map[string]FieldMapping{"link": {Type: "join", Relations: map[string]any{"parent": []string{"child"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		j       Join
		routing string
		ok      bool
	}{{Join{Name: "parent"}, "", true}, {Join{Name: "child", Parent: "1"}, "1", true}, {Join{Name: "child", Parent: "1"}, "", false}, {Join{Name: "unknown"}, "1", false}} {
		data, _ := json.Marshal(doc{tc.j})
		err := s.validateJoin(data, tc.routing)
		if (err == nil) != tc.ok {
			t.Fatal(tc, err)
		}
	}
	if err := s.requireJoinRouting(""); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
}
func TestIteratorLifecycle(t *testing.T) {
	ctx := context.Background()
	searches := 0
	closed := false
	r := testRepo(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/_pit") && req.Method == "POST":
			return response(200, `{"id":"pit1"}`), nil
		case req.Method == "DELETE":
			m := readBody(t, req)
			if string(m["id"]) != `"pit2"` {
				t.Fatal(m)
			}
			closed = true
			return response(200, `{}`), nil
		default:
			searches++
			if searches == 1 {
				return response(200, `{"pit_id":"pit2","hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_id":"1","_source":{"name":"Ada"},"sort":[9007199254740993]}]}}`), nil
			}
			m := readBody(t, req)
			if string(m["search_after"]) != `[9007199254740993]` {
				t.Fatal(m)
			}
			return response(200, `{"hits":{"hits":[]}}`), nil
		}
	})
	it, err := r.Iterate(ctx, NewSearch(MatchAll()), 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if !it.Next() || it.Hit().Source.Name != "Ada" || it.Next() || it.Err() != nil || !closed {
		t.Fatal(it.Err(), closed)
	}
	if err := it.Close(); err != nil {
		t.Fatal(err)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	closed = false
	r.client.transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == "DELETE" {
			if req.Context().Err() != nil {
				t.Fatal("cleanup canceled")
			}
			closed = true
			return response(200, `{}`), nil
		}
		return response(200, `{"id":"pit"}`), nil
	})
	it, err = r.Iterate(cancelCtx, Search{}, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if it.Next() || !errors.Is(it.Err(), context.Canceled) || !closed {
		t.Fatal(it.Err(), closed)
	}
}
func TestBulkPartialAndBackpressure(t *testing.T) {
	r := testRepo(t, func(req *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(req.Body)
		if !strings.HasSuffix(string(b), "\n") || strings.Count(string(b), "\n") != 3 {
			t.Fatal(string(b))
		}
		return response(200, `{"items":[{"index":{"_id":"1","status":201,"result":"created"}},{"delete":{"_id":"2","status":409,"error":{"type":"version_conflict_engine_exception","reason":"conflict"}}}]}`), nil
	})
	result, err := r.Bulk(context.Background(), []BulkOperation[testDoc]{{Action: BulkIndex, ID: "1", Document: testDoc{Name: "Ada"}}, {Action: BulkDelete, ID: "2"}}, "")
	var bulkErr *BulkError
	if !errors.As(err, &bulkErr) || bulkErr.Failed != 1 || len(result.Items) != 2 || !errors.Is(result.Items[1].Err, ErrConflict) {
		t.Fatal(result, err)
	}
	var requests atomic.Int32
	r.client.transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		return response(200, `{"items":[{"delete":{"_id":"1","status":200,"result":"deleted"}}]}`), nil
	})
	in := make(chan BulkOperation[testDoc], 10)
	for i := 0; i < 10; i++ {
		in <- BulkOperation[testDoc]{Action: BulkDelete, ID: "1"}
	}
	close(in)
	seen := map[int]bool{}
	err = r.BulkStream(context.Background(), in, BulkStreamOptions{BatchSize: 1, Workers: 2, QueueSize: 1}, func(batch BulkBatchResult) error {
		if batch.Err != nil {
			return batch.Err
		}
		seen[batch.Sequence] = true
		return nil
	})
	if err != nil || len(seen) != 10 || requests.Load() != 10 {
		t.Fatal(err, seen, requests.Load())
	}
}
func TestConcurrentBuilders(t *testing.T) {
	base := NewSearch(Term("active", true))
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := json.Marshal(base.Size(i).Highlight("name"))
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if len(base.fields) != 3 {
		t.Fatal(base.fields)
	}
}
func TestCursor(t *testing.T) {
	c := Cursor{PIT: "abc", Sort: []json.RawMessage{[]byte(`9007199254740993`), []byte(`"x"`)}}
	s, err := EncodeCursor(c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeCursor(s)
	if err != nil || !reflect.DeepEqual(c, got) {
		t.Fatal(got, err)
	}
	for _, raw := range []string{"!", strings.Repeat("x", 22001), "e30"} {
		if _, err := DecodeCursor(raw); err == nil {
			t.Fatal(raw)
		}
	}
}
