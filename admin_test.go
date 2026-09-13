package esodm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestAdminResources(t *testing.T) {
	ctx := context.Background()
	var path, method string
	c := testClient(func(req *http.Request) (*http.Response, error) {
		path, method = req.URL.Path, req.Method
		return response(200, `{"acknowledged":true,"dry_run":true}`), nil
	})
	a := c.Admin()
	for _, kind := range []ResourceKind{IndexTemplate, ComponentTemplate, IngestPipeline, LifecyclePolicy, DataStream} {
		var spec any = map[string]any{}
		if kind == DataStream {
			spec = nil
		}
		if err := a.Put(ctx, kind, "test", spec); err != nil || method != "PUT" || !strings.HasSuffix(path, "/test") {
			t.Fatal(err, path, method)
		}
		if _, err := a.Get(ctx, kind, "test"); err != nil {
			t.Fatal(err)
		}
		if err := a.Delete(ctx, kind, "test"); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []ResourceKind{"bad", IndexTemplate} {
		if err := a.Put(ctx, kind, "*", nil); err == nil {
			t.Fatal("invalid resource")
		}
	}
	if err := a.Put(ctx, DataStream, "test", map[string]any{}); err == nil {
		t.Fatal("data stream body")
	}
	if err := a.Put(ctx, IndexTemplate, "test", nil); err == nil {
		t.Fatal("missing spec")
	}
	if err := a.CreateIndex(ctx, "test", map[string]any{}, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteIndex(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Mapping(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	if err := a.PutMapping(ctx, "test", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err := a.PutSettings(ctx, "test", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err := a.Refresh(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	w := true
	if err := a.Aliases(ctx, AliasAction{Add: true, Index: "one", Alias: "alias", WriteIndex: &w}, AliasAction{Index: "two", Alias: "alias"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Aliases(ctx); err == nil {
		t.Fatal("empty actions")
	}
	if _, err := a.Rollover(ctx, "test", map[string]any{"max_docs": 1}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ExplainLifecycle(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SimulatePipeline(ctx, "test", []any{map[string]string{"x": "y"}}); err != nil {
		t.Fatal(err)
	}
	checks := []func() error{func() error { return a.CreateIndex(ctx, "*", nil, nil) }, func() error { return a.DeleteIndex(ctx, "*") }, func() error { _, e := a.Mapping(ctx, "*"); return e }, func() error { return a.PutMapping(ctx, "*", nil) }, func() error { return a.PutSettings(ctx, "*", nil) }, func() error { return a.Refresh(ctx, "*") }, func() error { return a.Aliases(ctx, AliasAction{Index: "*", Alias: "x"}) }, func() error { return a.Aliases(ctx, AliasAction{Index: "x", Alias: "*"}) }, func() error { _, e := a.Rollover(ctx, "*", nil, false); return e }, func() error { _, e := a.ExplainLifecycle(ctx, "*"); return e }, func() error { _, e := a.SimulatePipeline(ctx, "*", nil); return e }, func() error { _, e := a.Get(ctx, "bad", "test"); return e }, func() error { return a.Delete(ctx, "bad", "test") }}
	for _, check := range checks {
		if !errors.Is(check(), ErrValidation) {
			t.Fatal("invalid input accepted")
		}
	}
}
func TestMigrationSafety(t *testing.T) {
	ctx := context.Background()
	schema, err := NewSchema[testDoc]("logical")
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range []string{"", "mapping_changed", "alias_changed", "reindex_failure", "count_mismatch", "create_failure", "alias_during"} {
		t.Run(failure, func(t *testing.T) {
			applied := false
			aliasCalls := 0
			mappingCalls := 0
			moved := false
			created := false
			c := testClient(func(req *http.Request) (*http.Response, error) {
				switch {
				case req.URL.Path == "/_alias/logical":
					aliasCalls++
					source := "source"
					if failure == "alias_changed" && applied || failure == "alias_during" && aliasCalls >= 3 {
						source = "other"
					}
					return response(200, `{"`+source+`":{"aliases":{"logical":{"is_write_index":true}}}}`), nil
				case req.URL.Path == "/source/_mapping":
					mappingCalls++
					mapping := string(schema.mapping)
					if failure == "mapping_changed" && mappingCalls > 1 {
						mapping = `{"properties":{}}`
					}
					return response(200, `{"source":{"mappings":`+mapping+`}}`), nil
				case req.Method == "PUT" && req.URL.Path == "/target":
					if failure == "create_failure" {
						return response(400, `{"error":"exists"}`), nil
					}
					created = true
					return response(200, `{"acknowledged":true}`), nil
				case req.URL.Path == "/_reindex":
					if failure == "reindex_failure" {
						return response(200, `{"total":2,"created":1,"failures":[{}]}`), nil
					}
					return response(200, `{"total":2,"created":2,"failures":[]}`), nil
				case strings.HasSuffix(req.URL.Path, "/_count"):
					if failure == "count_mismatch" && strings.HasPrefix(req.URL.Path, "/target") {
						return response(200, `{"count":1}`), nil
					}
					return response(200, `{"count":2}`), nil
				case req.URL.Path == "/_aliases":
					moved = true
					return response(200, `{"acknowledged":true}`), nil
				default:
					return response(200, `{"acknowledged":true}`), nil
				}
			})
			plan, err := PlanMigration(ctx, c, "logical", "target", schema)
			if err != nil {
				t.Fatal(err)
			}
			summary := plan.Summary()
			summary.Steps[0] = "changed"
			if plan.Summary().Steps[0] == "changed" {
				t.Fatal("mutable plan")
			}
			if _, err := json.Marshal(plan); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Admin().ApplyMigration(ctx, plan, MigrationOptions{}); !errors.Is(err, ErrValidation) {
				t.Fatal(err)
			}
			applied = true
			result, err := c.Admin().ApplyMigration(ctx, plan, MigrationOptions{WritesPaused: true})
			if failure == "" {
				if err != nil || !result.AliasMoved || !moved || !created || result.Documents != 2 {
					t.Fatal(result, err)
				}
			} else if err == nil || moved {
				t.Fatal("unsafe alias switch", result, err)
			}
		})
	}
	if jsonEqual([]byte(`broken`), []byte(`{}`)) {
		t.Fatal("invalid JSON equal")
	}
}
func TestSearchPartialAndMSearch(t *testing.T) {
	ctx := context.Background()
	r := testRepo(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/_msearch" {
			return response(200, `{"responses":[{"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_source":{"name":"Ada"}}]}},{"status":400,"error":{"type":"parsing_exception","reason":"bad"}}]}`), nil
		}
		return response(200, `{"timed_out":true,"_shards":{"failed":1},"hits":{"hits":[]}}`), nil
	})
	_, err := r.Search(ctx, NewSearch(MatchAll()))
	var partial *PartialSearchError
	if !errors.As(err, &partial) {
		t.Fatal(err)
	}
	_ = partial.Error()
	r.hooks.AfterRead = func(_ context.Context, h *Hit[testDoc]) error { h.Source.Active = true; return nil }
	out, err := r.MultiSearch(ctx, Search{}, NewSearch(MatchAll()))
	if err != nil || len(out) != 2 || !out[0].Result.Hits.Hits[0].Source.Active || !errors.Is(out[1].Err, ErrValidation) {
		t.Fatal(out, err)
	}
	if empty, err := r.MultiSearch(ctx); err != nil || len(empty) != 0 {
		t.Fatal(empty, err)
	}
	r.client.transport = transportFunc(func(req *http.Request) (*http.Response, error) { return response(200, `{"responses":[]}`), nil })
	if _, err := r.MultiSearch(ctx, Search{}); err == nil {
		t.Fatal("count mismatch")
	}
}

func TestAcknowledgementAndPartialCount(t *testing.T) {
	ctx := context.Background()
	for _, body := range []string{`{}`, `{"acknowledged":false}`, `{"acknowledged":true,"errors":true}`} {
		c := testClient(func(*http.Request) (*http.Response, error) { return response(200, body), nil })
		if err := c.Admin().PutSettings(ctx, "test", map[string]any{}); !errors.Is(err, ErrUnacknowledged) {
			t.Fatal(body, err)
		}
	}
	r := testRepo(t, func(*http.Request) (*http.Response, error) {
		return response(200, `{"count":3,"_shards":{"failed":1}}`), nil
	})
	count, err := r.Count(ctx, MatchAll())
	var partial *PartialSearchError
	if count != 3 || !errors.As(err, &partial) {
		t.Fatal(count, err)
	}
	if jsonEqual([]byte(`{"n":9007199254740992}`), []byte(`{"n":9007199254740993}`)) {
		t.Fatal("mapping integer precision lost")
	}
}

func TestFailedRefresh(t *testing.T) {
	c := testClient(func(*http.Request) (*http.Response, error) { return response(200, `{"_shards":{"failed":1}}`), nil })
	var partial *PartialSearchError
	if err := c.Admin().Refresh(context.Background(), "test"); !errors.As(err, &partial) {
		t.Fatal(err)
	}
}
