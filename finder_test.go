package esodm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestFinderComposition(t *testing.T) {
	r := testRepo(t, func(*http.Request) (*http.Response, error) { t.Fatal("unexpected request"); return nil, nil })
	for _, tc := range []struct {
		name  string
		f     Finder[testDoc]
		query string
	}{
		{"empty", r.Find(), `{"match_all":{}}`},
		{"single", r.Find(Term("age", 4)), `{"term":{"age":{"value":4}}}`},
		{"should", r.Find().Should(Term("age", 4)), `{"bool":{"minimum_should_match":1,"should":[{"term":{"age":{"value":4}}}]}}`},
		{"filter should", r.Find().Filter(Term("active", true)).Should(Term("age", 4)), `{"bool":{"filter":[{"term":{"active":{"value":true}}}],"should":[{"term":{"age":{"value":4}}}]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.f.Search())
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]json.RawMessage
			_ = json.Unmarshal(data, &body)
			var got, want any
			_ = json.Unmarshal(body["query"], &got)
			_ = json.Unmarshal([]byte(tc.query), &want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s != %s", body["query"], tc.query)
			}
		})
	}
	base := r.Find(Term("age", 4))
	before, _ := json.Marshal(base.Search())
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			f := base.Must(Term("age", i)).Filter(Term("active", true)).Not(MatchNone()).Should(MatchAll())
			if f.Err() != nil {
				t.Error(f.Err())
			}
			_, _ = json.Marshal(f.Search())
		})
	}
	wg.Wait()
	after, _ := json.Marshal(base.Search())
	if string(before) != string(after) {
		t.Fatal("finder mutated")
	}
	f := r.FindSearch(NewSearch(Term("age", 4)).Aggregations(map[string]Aggregation{"old": Avg("age")})).Aggregate("new", Sum("age")).Aggregate("old", Max("age")).Filter(MatchAll())
	var aggs map[string]json.RawMessage
	_ = json.Unmarshal(f.Search().fields["aggs"], &aggs)
	if len(aggs) != 2 || !strings.Contains(string(aggs["old"]), "max") {
		t.Fatal(aggs)
	}
	for _, f := range []Finder[testDoc]{r.Find(Term("", 1)), r.FindSearch(Search{}.Size(-1)), r.Find().Aggregate("", Avg("age")), r.Find().Aggregate("x", Aggregation{err: ErrValidation}), {}} {
		if f.Err() == nil {
			t.Fatal("missing validation")
		}
		if _, err := f.All(context.Background()); err == nil {
			t.Fatal("executed invalid finder")
		}
	}
}

func TestFinderTerminals(t *testing.T) {
	ctx := context.Background()
	calls := 0
	r := testRepo(t, func(req *http.Request) (*http.Response, error) {
		calls++
		body := readBody(t, req)
		if strings.HasSuffix(req.URL.Path, "/_count") {
			if req.URL.Query().Get("routing") != "a,b" || req.URL.Query().Get("preference") != "local" {
				t.Fatal(req.URL)
			}
			return response(200, `{"count":25}`), nil
		}
		if _, ok := body["terminate_after"]; ok {
			if string(body["size"]) != "1" || string(body["track_total_hits"]) != "false" || body["from"] != nil {
				t.Fatal(body)
			}
		}
		return response(200, `{"hits":{"total":{"value":25,"relation":"eq"},"hits":[{"_id":"1","_source":{"name":"Ada"}}]}}`), nil
	})
	f := r.Find().Routing("a", "b").Preference("local")
	if n, err := f.Count(ctx); err != nil || n != 25 {
		t.Fatal(n, err)
	}
	if h, err := f.Size(20).First(ctx); err != nil || h.ID != "1" {
		t.Fatal(h, err)
	}
	if ok, err := f.From(10).Exists(ctx); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if docs, err := f.Sources(ctx); err != nil || len(docs) != 1 || docs[0].Name != "Ada" {
		t.Fatal(docs, err)
	}
	if p, err := f.Page(ctx, 2, 10); err != nil || !p.Exact || !p.HasNext || p.Number != 2 || p.Sources()[0].Name != "Ada" {
		t.Fatal(p, err)
	}
	for _, args := range [][2]int{{0, 10}, {1, 0}, {int(^uint(0) >> 1), 2}} {
		if _, err := f.Page(ctx, args[0], args[1]); !errors.Is(err, ErrValidation) {
			t.Fatal(err)
		}
	}
	if calls != 5 {
		t.Fatal(calls)
	}
	empty := testRepo(t, func(*http.Request) (*http.Response, error) { return response(200, `{"hits":{"hits":[]}}`), nil })
	if _, err := empty.Find().First(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if ok, err := empty.Find().Exists(ctx); err != nil || ok {
		t.Fatal(ok, err)
	}
	partial := testRepo(t, func(*http.Request) (*http.Response, error) {
		return response(200, `{"timed_out":true,"hits":{"hits":[{"_id":"1"}]}}`), nil
	})
	if hits, err := partial.Find().All(ctx); err == nil || len(hits) != 1 {
		t.Fatal(hits, err)
	}
}

func TestFinderEachOwnership(t *testing.T) {
	closed := 0
	r := testRepo(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == "DELETE":
			closed++
			return response(200, `{"succeeded":true}`), nil
		case strings.HasSuffix(req.URL.Path, "/_pit"):
			return response(200, `{"id":"pit"}`), nil
		default:
			return response(200, `{"pit_id":"pit","hits":{"hits":[{"_id":"1","_source":{"name":"Ada"},"sort":[1]}]}}`), nil
		}
	})
	for hit, err := range r.Find().Each(context.Background(), 10) {
		if err != nil || hit.ID != "1" {
			t.Fatal(hit, err)
		}
		break
	}
	for doc, err := range r.Find().EachSource(context.Background(), 10) {
		if err != nil || doc.Name != "Ada" {
			t.Fatal(doc, err)
		}
		break
	}
	if closed != 2 {
		t.Fatal(closed)
	}
	n := 0
	for _, err := range (Finder[testDoc]{}).Each(context.Background(), 10) {
		n++
		if !errors.Is(err, ErrValidation) {
			t.Fatal(err)
		}
	}
	if n != 1 {
		t.Fatal(n)
	}
}

func TestFinderVectorAndPostFilterSemantics(t *testing.T) {
	r := testRepo(t, func(req *http.Request) (*http.Response, error) {
		body := readBody(t, req)
		if body["post_filter"] != nil && body["terminate_after"] != nil {
			t.Fatal("early termination can hide post-filter matches")
		}
		return response(200, `{"hits":{"hits":[{"_id":"match"}]}}`), nil
	})
	vector := r.Find().KNN(KNN{Field: "embedding", Vector: []float32{1, 2}, K: 1, Candidates: 2}).Search()
	if vector.fields["query"] != nil {
		t.Fatal("pure vector query broadened by implicit match-all")
	}
	if ok, err := r.Find().With("post_filter", Term("age", 99)).Exists(context.Background()); err != nil || !ok {
		t.Fatal(ok, err)
	}
	readOnly, err := r.WithIndex("people-*")
	if err != nil {
		t.Fatal(err)
	}
	readOnly.hooks.BeforePatch = func(context.Context, string, Patch) error { t.Fatal("invalid write invoked hook"); return nil }
	readOnly.hooks.Validate = func(context.Context, *testDoc) error { t.Fatal("invalid upsert invoked hook"); return nil }
	if _, err := readOnly.Update(context.Background(), "1", Patch{"age": 1}); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	if _, err := readOnly.Upsert(context.Background(), "1", Patch{"age": 1}, testDoc{}); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
}
