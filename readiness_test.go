package esodm

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	search8 "github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

func TestReadinessOptionsAndDiscovery(t *testing.T) {
	s, err := NewSchema[testDoc]("people", WithDynamic(DynamicFalse), WithDynamic(DynamicStrict), WithProperties(map[string]FieldMapping{"age": {Type: "long"}}), WithSettings(map[string]any{"number_of_shards": 1}))
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRepository(testClient(func(req *http.Request) (*http.Response, error) { return response(201, `{"_id":"1"}`), nil }), s, WithHooks(Hooks[testDoc]{}))
	if err != nil {
		t.Fatal(err)
	}
	seq, term := int64(1), int64(2)
	option := IfMatches(Metadata{SeqNo: &seq, PrimaryTerm: &term, Routing: "a"})
	seq = 9
	options, err := writeOptions([]WriteOption{WithRefresh(RefreshWaitFor), option, WithPipeline("p")})
	if err != nil || *options.IfSeqNo != 1 || options.Refresh != RefreshWaitFor || options.Pipeline != "p" {
		t.Fatal(options, err)
	}
	*options.IfSeqNo = 99
	again, _ := writeOptions([]WriteOption{option})
	if *again.IfSeqNo != 1 {
		t.Fatal("option alias")
	}
	if _, err := r.Create(context.Background(), "1", testDoc{}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Replace(context.Background(), "1", testDoc{}, nil); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	if _, err := NewRepository(r.client, s, RepositoryOption[testDoc](nil)); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	realtime := false
	r.client.transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		q := req.URL.Query()
		if q.Get("routing") != "a" && req.URL.Path != "/_mget" {
			t.Fatal(q)
		}
		if q.Get("preference") != "local" || q.Get("realtime") != "false" || q.Get("_source_includes") != "name" || q.Get("_source_excludes") != "age" {
			t.Fatal(q)
		}
		if req.URL.Path == "/_mget" {
			return response(200, `{"docs":[{"_id":"1","found":true}]}`), nil
		}
		return response(200, `{"_id":"1","found":true}`), nil
	})
	read := ReadOptions{Routing: "a", Preference: "local", Realtime: &realtime, Source: &types.SourceFilter{Includes: []string{"name"}, Excludes: []string{"age"}}}
	if _, err := r.GetWith(context.Background(), "1", read); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ExistsWith(context.Background(), "1", read); err != nil {
		t.Fatal(err)
	}
	if _, err := r.MGetWith(context.Background(), []string{"1"}, read); err != nil {
		t.Fatal(err)
	}
	transport := TransportFunc(func(*http.Request) (*http.Response, error) { t.Fatal("discovery request"); return nil, nil })
	c, err := ConnectVersion(context.Background(), transport, Version{9, 5, 2}, Config{})
	if err != nil || c.Version().Major != 9 {
		t.Fatal(c, err)
	}
	if err := c.DoTyped(context.Background(), search8.New(nil), nil); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	for _, v := range []Version{{7, 10, 0}, {9, 4, 0}, {9, 5, -1}} {
		if _, err := ConnectVersion(context.Background(), transport, v, Config{}); !errors.Is(err, ErrUnsupported) {
			t.Fatal(err)
		}
	}
	if _, err := ConnectVersion(context.Background(), nil, Version{9, 5, 2}, Config{}); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	if _, err := ConnectVersion(context.Background(), transport, Version{9, 5, 2}, Config{MaxResponseBytes: -1}); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ConnectVersion(ctx, transport, Version{9, 5, 2}, Config{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestReadinessIndexAndDrift(t *testing.T) {
	for _, name := range []string{"People", "-index", "_index", "+index", "a:b", "bad|index", strings.Repeat("a", 256), "a\tindex"} {
		if err := validateIndexName(name); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	r := testRepo(t, func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/_mapping") {
			return response(200, `{"people":{"mappings":{"dynamic":false,"properties":{"name":{"type":"keyword"},"age":{"type":"long"},"extra":{"type":"boolean"}}}}}`), nil
		}
		return response(200, `{"hits":{"hits":[{"_index":"people-1","_id":"1"}]}}`), nil
	})
	derived, err := r.WithIndex("people-*,remote:archive")
	if err != nil {
		t.Fatal(err)
	}
	if r.Index() != "people" {
		t.Fatal("base changed")
	}
	result, err := derived.Find().All(context.Background())
	if err != nil || result[0].Index != "people-1" {
		t.Fatal(result, err)
	}
	if _, err := derived.Create(context.Background(), "1", testDoc{}); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	if err := derived.EnsureIndex(context.Background()); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	for _, name := range []string{"a,,b", "remote:", "a:b:c", ""} {
		if _, err := r.WithIndex(name); !errors.Is(err, ErrValidation) {
			t.Fatal(name, err)
		}
	}
	drift, err := r.VerifyIndex(context.Background())
	if err != nil || drift.Matches() || len(drift.ChangedFields) != 1 || len(drift.MissingFields) != 2 || len(drift.ExtraFields) != 1 || len(drift.ChangedOptions) != 1 {
		t.Fatal(drift, err)
	}
	var registry Registry
	if err := registry.Add(r); err != nil {
		t.Fatal(err)
	}
	if got, err := registry.VerifyAll(context.Background()); err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
}

func TestReadinessTelemetryAndRedaction(t *testing.T) {
	for path, want := range map[string]string{"/p/_update_by_query": "update_by_query", "/p/_delete_by_query": "delete_by_query", "/p/_mapping": "mapping", "/p/_settings": "settings", "/p/_refresh": "refresh", "/_tasks/node:1": "tasks.get", "/_tasks/node:1/_cancel": "tasks.cancel", "/_aliases": "aliases", "/_index_template/x": "template.index", "/_component_template/x": "template.component", "/_ingest/pipeline/x/_simulate": "pipeline.simulate", "/p/_ilm/explain": "ilm.explain", "/p/_mget": "mget", "/p/_bulk": "bulk", "/p/_rollover": "rollover"} {
		req, _ := http.NewRequest("POST", path, nil)
		got, _ := requestOperation(req)
		if got != want {
			t.Fatal(path, got, want)
		}
	}
	private := "private document"
	e := &Error{Status: 400, Type: "parse_exception", Reason: private, Body: json.RawMessage(`{"secret":true}`), Cause: types.ErrorCause{Reason: &private}}
	var output strings.Builder
	slog.New(slog.NewJSONHandler(&output, nil)).Error("failure", "error", e)
	if strings.Contains(output.String(), "private") || strings.Contains(output.String(), "secret") {
		t.Fatal(output.String())
	}
	redacted := e.Redacted()
	if redacted.Reason != "" || redacted.Body != nil || redacted.Cause.Reason != nil || redacted.Status != 400 {
		t.Fatal(redacted)
	}
	var nilError *Error
	if nilError.Redacted() != nil {
		t.Fatal("nil redaction")
	}
	_ = nilError.LogValue()
}

type invalidAudit struct {
	Timestamps `json:"audit"`
}

func TestStampSchemaAndInnerHits(t *testing.T) {
	if _, err := NewSchema[invalidAudit]("audit"); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	type valid struct {
		Timestamps
		Name string `json:"name"`
	}
	if _, err := NewSchema[valid]("audit"); err != nil {
		t.Fatal(err)
	}
	h := Hit[testDoc]{InnerHits: map[string]json.RawMessage{"children": json.RawMessage(`{"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_source":{"name":"Ada"}}]}}`)}}
	if got, err := InnerHits[testDoc](h, "children"); err != nil || got.Sources()[0].Name != "Ada" {
		t.Fatal(got, err)
	}
	if _, err := InnerHits[testDoc](h, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	h.InnerHits["children"] = json.RawMessage("{")
	if _, err := InnerHits[testDoc](h, "children"); err == nil {
		t.Fatal("invalid JSON")
	}
}

func TestGeoNegativeAndOfficialBounds(t *testing.T) {
	for _, q := range []Query{Geo("").WithinBox(nil), Geo("location").WithinDistance("", GeoPoint{Lat: 1, Lon: 2}), Geo("location").WithinDistance("1km", nil), Percolate(types.PercolateQuery{}), Percolate(types.PercolateQuery{Field: "query", Document: json.RawMessage(`{}`), Documents: []json.RawMessage{json.RawMessage(`{}`)}})} {
		if _, err := q.MarshalJSON(); err == nil {
			t.Fatal("invalid geo/percolate accepted")
		}
	}
	box := Geo("location").WithinBox(types.TopLeftBottomRightGeoBounds{TopLeft: GeoPoint{Lat: 2, Lon: 1}, BottomRight: GeoPoint{Lat: 1, Lon: 2}})
	if _, err := box.MarshalJSON(); err != nil {
		t.Fatal(err)
	}
	if _, err := GeoShape(types.GeoShapeQuery{}).MarshalJSON(); err != nil {
		t.Fatal(err)
	}
	id := "1"
	if _, err := Percolate(types.PercolateQuery{Field: "query", Id: &id}).MarshalJSON(); err != nil {
		t.Fatal(err)
	}
}

func TestExtendedMappingTags(t *testing.T) {
	type tagged struct {
		Name     string         `json:"name" es:"type=text,index=true,fields=keyword,copy_to=all|title"`
		Code     string         `json:"code" es:"doc_values=false,ignore_above=100"`
		Missing  int            `json:"missing" es:"null_value=0"`
		Vector   []float32      `json:"vector" es:"type=dense_vector,dims=3,similarity=cosine"`
		Object   map[string]any `json:"object" es:"type=object,dynamic=strict"`
		Semantic string         `json:"semantic" es:"type=semantic_text,inference_id=test-endpoint"`
	}
	schema, err := NewSchema[tagged]("tagged")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"doc_values":false`, `"ignore_above":100`, `"null_value":0`, `"dims":3`, `"inference_id":"test-endpoint"`} {
		if !strings.Contains(string(schema.Mapping()), expected) {
			t.Fatal(expected, string(schema.Mapping()))
		}
	}
	for _, tag := range []string{"index=yes", "doc_values=0", "dims=-1", "ignore_above=x", "copy_to=a|", "fields=text", "dynamic=unknown", "null_value=[]", "null_value={}", "null_value=bad"} {
		if _, _, err := parseTag(tag); !errors.Is(err, ErrValidation) {
			t.Fatal(tag, err)
		}
	}
	if _, err := NewSchema[tagged]("tagged", WithProperties(map[string]FieldMapping{"vector": {Type: "dense_vector", Dims: 4, Similarity: "cosine"}})); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	if _, err := Semantic("semantic", "search text").MarshalJSON(); err != nil {
		t.Fatal(err)
	}
}
