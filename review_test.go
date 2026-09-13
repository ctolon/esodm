package esodm

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/elastic/elastic-transport-go/v8/elastictransport"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
	"net/http"
	"strings"
	"testing"
	"time"
)

type ReviewBase struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type reviewEmbedded struct {
	*ReviewBase
	Name int `json:"name"`
}

func TestReviewEmbedded(t *testing.T) {
	s, err := NewSchema[reviewEmbedded]("embedded")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Properties map[string]FieldMapping `json:"properties"`
	}
	if err = json.Unmarshal(s.Mapping(), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Properties) != 2 || m.Properties["name"].Type != "long" {
		t.Fatalf("%s", s.Mapping())
	}
	type Left struct{ Value string }
	type Right struct{ Value int }
	type Ambiguous struct {
		Left
		Right
	}
	if _, err = NewSchema[Ambiguous]("ambiguous"); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	type Tagged struct {
		Renamed string `json:"Value"`
	}
	type Dominated struct {
		Left
		Tagged
	}
	if _, err = NewSchema[Dominated]("dominated"); err != nil {
		t.Fatal(err)
	}
}

func TestReviewSearchParams(t *testing.T) {
	base := NewSearch(MatchAll())
	s := base.Routing("a", "b").Preference("session").RequestCache(true).TypedKeys(true).Timeout(time.Nanosecond).MinScore(1)
	p := s.Params()
	p.Set("routing", "mutated")
	if base.Params().Get("routing") != "" || s.Params().Get("routing") != "a,b" {
		t.Fatal("aliased params")
	}
	r := testRepo(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("routing") != "a,b" || req.URL.Query().Get("typed_keys") != "true" {
			t.Fatal(req.URL)
		}
		body := readBody(t, req)
		if string(body["timeout"]) != `"1ms"` {
			t.Fatal(body)
		}
		return response(200, `{"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_source":{"name":"Ada"}}]},"aggregations":{"avg#age":{"value":12}}}`), nil
	})
	result, err := r.Search(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if result.Len() != 1 || result.Total() != 1 || !result.Exact() || result.Sources()[0].Name != "Ada" {
		t.Fatal(result)
	}
	aggs, err := result.Aggregates()
	if err != nil || len(aggs) != 1 {
		t.Fatal(aggs, err)
	}
	avg, err := DecodeAgg[struct{ Value float64 }](result.Aggregations, "age")
	if err != nil || avg.Value != 12 {
		t.Fatal(avg, err)
	}
	for _, bad := range []Search{base.Param("filter_path", "x"), base.Timeout(0), base.Routing("x").PIT("pit", "1m")} {
		if _, err = r.Search(context.Background(), bad); !errors.Is(err, ErrValidation) {
			t.Fatal(err)
		}
	}
}

func TestReviewCursorOwnership(t *testing.T) {
	closes := 0
	searches := 0
	r := testRepo(t, func(req *http.Request) (*http.Response, error) {
		if req.Method == "DELETE" {
			closes++
			return response(200, `{}`), nil
		}
		if strings.HasSuffix(req.URL.Path, "/_pit") {
			if req.URL.Query().Get("routing") != "route" {
				t.Fatal(req.URL)
			}
			return response(200, `{"id":"first"}`), nil
		}
		searches++
		body := readBody(t, req)
		if req.URL.Query().Get("routing") != "" {
			t.Fatal("PIT routing leaked")
		}
		if searches == 2 {
			if string(body["search_after"]) != "[1]" {
				t.Fatal(string(body["search_after"]))
			}
			return response(200, `{"pit_id":"third","hits":{"hits":[{"_id":"2","sort":[2]}]}}`), nil
		}
		return response(200, `{"pit_id":"second","hits":{"hits":[{"_id":"1","sort":[1]},{"_id":"2","sort":[2]}]}}`), nil
	})
	it, err := r.Iterate(context.Background(), NewSearch(MatchAll()).Routing("route"), 2, "1m")
	if err != nil {
		t.Fatal(err)
	}
	if !it.Next() {
		t.Fatal(it.Err())
	}
	c, err := it.Cursor()
	if err != nil {
		t.Fatal(err)
	}
	c.Sort[0][0] = '9'
	c, err = it.Detach()
	if err != nil || string(c.Sort[0]) != "1" || c.PIT != "second" {
		t.Fatal(c, err)
	}
	_ = it.Close()
	if closes != 0 {
		t.Fatal(closes)
	}
	resumed, err := r.ResumeIterator(context.Background(), NewSearch(MatchAll()), c, 2, "1m")
	if err != nil {
		t.Fatal(err)
	}
	for hit, err := range resumed.Each() {
		if err != nil || hit.ID != "2" {
			t.Fatal(hit, err)
		}
		break
	}
	if closes != 1 || resumed.Err() != nil {
		t.Fatal(closes, resumed.Err())
	}
	if _, err = resumed.Cursor(); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
}

func TestReviewErrorsAndTelemetry(t *testing.T) {
	c := testClient(func(*http.Request) (*http.Response, error) {
		return response(429, `{"error":{"type":"es_rejected_execution_exception","reason":"busy","caused_by":{"type":"inner","reason":"cause"}}}`), nil
	})
	err := c.Do(context.Background(), "GET", "/people/_doc/private", nil, nil, nil)
	var ee *Error
	if !errors.As(err, &ee) || !errors.Is(err, ErrTooManyRequests) || !ee.Retryable() || ee.Cause.CausedBy == nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", "https://user:password@host/people/_doc/private?routing=secret", strings.NewReader("sensitive"))
	req.Header.Set("Authorization", "secret")
	safe := telemetryRequest(req, "index")
	if safe.URL.String() != "https://host/index" || safe.Body != nil || len(safe.Header) != 0 {
		t.Fatal(safe)
	}
	if op, index := requestOperation(req); op != "index" || index != "people" {
		t.Fatal(op, index)
	}
	if (Version{9, 5, 2}).Compare(Version{9, 4, 5}) != 1 || (Version{8, 19, 7}).Compare(Version{8, 19, 7}) != 0 {
		t.Fatal("comparison")
	}
	seq, term := int64(1), int64(2)
	meta := Metadata{Routing: "r", SeqNo: &seq, PrimaryTerm: &term}
	opts := meta.Conditional()
	*opts.IfSeqNo = 9
	if seq != 1 || opts.Routing != "r" {
		t.Fatal(opts)
	}
	_, err = TransportFunc(func(*http.Request) (*http.Response, error) { return nil, ErrNotFound }).Perform(req)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestReviewPatchAndHooks(t *testing.T) {
	field := NewField[string]("name")
	value := "Ada"
	patch, err := NewPatch(field.Set(value), NewField[*string]("optional").Clear())
	if err != nil {
		t.Fatal(err)
	}
	if field.Desc().Desc != true || field.Asc().Desc || Text("name").Keyword().name != "name.keyword" {
		t.Fatal("descriptors")
	}
	for _, a := range [][]Assignment{{field.Set("a"), field.Set("b")}, {NewField[string]("nested.name").Set("a")}, {{}}, {NewField[any]("x").Set(make(chan int))}} {
		if _, err := NewPatch(a...); !errors.Is(err, ErrValidation) {
			t.Fatal(err)
		}
	}
	calls := 0
	r := testRepo(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method == "POST" {
			b := readBody(t, req)
			if !strings.Contains(string(b["doc"]), "changed") {
				t.Fatal(b)
			}
		}
		return response(200, `{"result":"updated"}`), nil
	})
	r.hooks.BeforePatch = func(_ context.Context, id string, p Patch) error { p["name"] = "changed"; return nil }
	if _, err = r.Update(context.Background(), "1", patch, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if string(patch["name"].(json.RawMessage)) != `"Ada"` {
		t.Fatal("hook mutated input")
	}
	hookErr := errors.New("veto")
	r.hooks.BeforeDelete = func(context.Context, string) error { return hookErr }
	if _, err = r.Delete(context.Background(), "1", WriteOptions{}); !errors.Is(err, hookErr) {
		t.Fatal(err)
	}
	if _, _, err = r.EncodeOperation(context.Background(), BulkOperation[testDoc]{Action: BulkDelete, ID: "1"}); !errors.Is(err, hookErr) {
		t.Fatal(err)
	}
	r.hooks.BeforePatch = func(context.Context, string, Patch) error { return hookErr }
	if _, err = r.Update(context.Background(), "1", patch, WriteOptions{}); !errors.Is(err, hookErr) {
		t.Fatal(err)
	}
	if _, _, err = r.EncodeOperation(context.Background(), BulkOperation[testDoc]{Action: BulkUpdate, ID: "1", Patch: patch}); !errors.Is(err, hookErr) {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	for _, q := range []Query{Date("created").GT(now), Date("created").GTE(now), Date("created").LT(now), Date("created").LTE(now), Date("created").Between(now, now)} {
		b, err := json.Marshal(q)
		if err != nil || !strings.Contains(string(b), "2026-09-06") {
			t.Fatal(string(b), err)
		}
	}
}

func TestReviewTimedBulk(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	input := make(chan BulkOperation[testDoc], 1)
	input <- BulkOperation[testDoc]{Action: BulkIndex, ID: "1"}
	r := testRepo(t, func(*http.Request) (*http.Response, error) {
		return response(200, `{"items":[{"index":{"_id":"1","status":201}}]}`), nil
	})
	flushed := false
	err := r.BulkStream(ctx, input, BulkStreamOptions{BatchSize: 10, FlushInterval: time.Millisecond}, func(result BulkBatchResult) error { flushed = true; close(input); return result.Err })
	if err != nil || !flushed {
		t.Fatal(flushed, err)
	}
}

type reviewInstrument struct {
	elastictransport.Instrumentation
	events []string
	t      *testing.T
}

func (i *reviewInstrument) Start(ctx context.Context, name string) context.Context {
	i.events = append(i.events, "start:"+name)
	return context.WithValue(ctx, reviewContextKey{}, true)
}

type reviewContextKey struct{}

func (i *reviewInstrument) Close(context.Context) { i.events = append(i.events, "close") }
func (i *reviewInstrument) RecordError(_ context.Context, err error) {
	if strings.Contains(err.Error(), "secret") {
		i.t.Fatal(err)
	}
	i.events = append(i.events, "error")
}
func (i *reviewInstrument) RecordPathPart(_ context.Context, key, value string) {
	if key != "index" || value != "people" {
		i.t.Fatal(key, value)
	}
	i.events = append(i.events, "index")
}
func (i *reviewInstrument) BeforeRequest(req *http.Request, _ string) {
	if req.Context().Value(reviewContextKey{}) != true || req.URL.RawQuery != "" || req.Body != nil {
		i.t.Fatal(req)
	}
	i.events = append(i.events, "before")
}
func (i *reviewInstrument) AfterRequest(req *http.Request, _, _ string) {
	if strings.Contains(req.URL.String(), "secret") {
		i.t.Fatal(req.URL)
	}
	i.events = append(i.events, "after")
}

type reviewTransport struct {
	Transport
	instrument *reviewInstrument
}

func (t reviewTransport) InstrumentationEnabled() elastictransport.Instrumentation {
	return t.instrument
}
func TestReviewNativeInstrumentation(t *testing.T) {
	i := &reviewInstrument{t: t}
	c := testClient(func(req *http.Request) (*http.Response, error) {
		if req.Context().Value(reviewContextKey{}) != true {
			t.Fatal("context not propagated")
		}
		return response(503, `{"error":"secret"}`), nil
	})
	c.transport = reviewTransport{c.transport, i}
	if err := c.Do(context.Background(), "GET", "/people/_doc/secret", nil, nil, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if strings.Join(i.events, ",") != "start:get,index,before,after,error,close" {
		t.Fatal(i.events)
	}
}

func TestReviewJoinCompilation(t *testing.T) {
	for _, relations := range []map[string]any{{}, {"": "c"}, {"p": 42}, {"p": []any{1}}, {"p": []string{}}, {"p": "p"}, {"p": ""}} {
		if _, err := compileJoin(map[string]FieldMapping{"j": {Type: "join", Relations: relations}}); !errors.Is(err, ErrValidation) {
			t.Fatal(relations, err)
		}
	}
	if _, err := compileJoin(map[string]FieldMapping{"a": {Type: "join", Relations: map[string]any{"p": "c"}}, "b": {Type: "join", Relations: map[string]any{"p": "c"}}}); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	j, err := compileJoin(map[string]FieldMapping{"j": {Type: "join", Relations: map[string]any{"root": []string{"middle"}, "middle": []any{"leaf"}}}})
	if err != nil {
		t.Fatal(err)
	}
	s := &Schema[testDoc]{join: j}
	for _, body := range []string{`{"j":"middle"}`, `{"j":{"name":"middle"}}`, `{"j":null}`, `[]`, `{`} {
		if err := s.validateJoin([]byte(body), "route"); !errors.Is(err, ErrValidation) {
			t.Fatal(body, err)
		}
	}
	if err := s.validateJoin([]byte(`{"j":{"name":"middle","parent":"root-id"}}`), "root-id"); err != nil {
		t.Fatal(err)
	}
}

func TestReviewMultiSearchParams(t *testing.T) {
	r := testRepo(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("typed_keys") != "true" {
			t.Fatal(req.URL)
		}
		var header map[string]any
		if err := json.NewDecoder(req.Body).Decode(&header); err != nil {
			t.Fatal(err)
		}
		if header["routing"] != "r" || header["request_cache"] != true || header["ignore_unavailable"] != false {
			t.Fatal(header)
		}
		return response(200, `{"responses":[{"hits":{"hits":[]}}]}`), nil
	})
	s := NewSearch(MatchAll()).Routing("r").Preference("p").RequestCache(true).TypedKeys(true).Param("ignore_unavailable", "false").Param("expand_wildcards", "open")
	if _, err := r.MultiSearch(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	for _, searches := range [][]Search{{s, s.TypedKeys(false)}, {s.Param("request_cache", "invalid")}, {s.Param("terminate_after", "1")}, {s.PIT("pit", "1m")}} {
		if _, err := r.MultiSearch(context.Background(), searches...); err == nil {
			t.Fatal("accepted invalid msearch")
		}
	}
	if !errors.Is(s.Param("filter_path", "x").Param("a", "b").Err(), ErrValidation) {
		t.Fatal("lost error")
	}
	if MatchAll().Err() != nil || (Aggregation{}).Err() == nil {
		t.Fatal("deferred errors")
	}
	if s.SortBy().SuggestWith("term", types.FieldSuggester{}).Err() != nil {
		t.Fatal("typed options")
	}
}

func TestReviewResumeValidation(t *testing.T) {
	r := testRepo(t, func(*http.Request) (*http.Response, error) { t.Fatal("unexpected transport"); return nil, nil })
	c := Cursor{PIT: "pit", Sort: []json.RawMessage{json.RawMessage("1")}}
	for _, tc := range []struct {
		s    Search
		c    Cursor
		n    int
		keep string
	}{{Search{}, Cursor{}, 1, ""}, {Search{}, c, 0, ""}, {Search{}, c, 1, "bad"}, {Search{}.With("from", 0), c, 1, ""}, {Search{}.Routing("r"), c, 1, ""}, {Search{}.With("bad", make(chan int)), c, 1, ""}} {
		if _, err := r.ResumeIterator(context.Background(), tc.s, tc.c, tc.n, tc.keep); err == nil {
			t.Fatal(tc)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	it, err := r.ResumeIterator(ctx, Search{}, c, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	it.closed = true
	if _, err = it.Detach(); err == nil {
		t.Fatal("closed detach")
	}
}

type reviewCountedDocument struct {
	Relation Join `json:"relation" es:"type=join"`
	Calls    *int `json:"-"`
}

func (d reviewCountedDocument) MarshalJSON() ([]byte, error) {
	*d.Calls++
	return json.Marshal(struct {
		Relation Join `json:"relation"`
	}{d.Relation})
}
func TestReviewSingleDocumentEncoding(t *testing.T) {
	s, err := NewSchema[reviewCountedDocument]("join", SchemaConfig{Properties: map[string]FieldMapping{"relation": {Type: "join", Relations: map[string]any{"parent": "child"}}}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRepository(testClient(func(*http.Request) (*http.Response, error) { return response(201, `{"result":"created"}`), nil }), s)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	doc := reviewCountedDocument{Relation: Join{Name: "child", Parent: "parent-id"}, Calls: &calls}
	if _, err = r.Create(context.Background(), "child-id", doc, WriteOptions{Routing: "parent-id"}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	calls = 0
	header, body, err := r.EncodeOperation(context.Background(), BulkOperation[reviewCountedDocument]{Action: BulkIndex, ID: "child-id", Document: doc, Options: WriteOptions{Routing: "parent-id"}})
	if err != nil || calls != 1 || !json.Valid(header) || !json.Valid(body) {
		t.Fatal(calls, string(header), string(body), err)
	}
}
