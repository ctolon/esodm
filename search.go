package esodm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	typedsearch "github.com/elastic/go-elasticsearch/v9/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v9/typedapi/esdsl"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

// Total reports the hit count and whether it is exact (eq) or a lower bound (gte).
type Total struct {
	// Value is the reported hit count or lower bound; inspect Relation.
	Value int64 `json:"value"`
	// Relation is eq for an exact count or gte for a lower bound; empty means no total was reported.
	Relation string `json:"relation"`
}

// SearchResult contains typed documents, metadata and raw aggregation results.
// A non-nil search error can accompany a partially populated result.
type SearchResult[T any] struct {
	// Took is server search processing time in milliseconds.
	Took int64 `json:"took"`
	// TimedOut reports server timeout and makes the result incomplete.
	TimedOut bool `json:"timed_out"`
	// PITID is the newest PIT identifier; it can change between pages. Empty means none was
	// returned.
	PITID string `json:"pit_id,omitempty"`
	// Hits contains this page, its total count and maximum score.
	Hits struct {
		// Total is the server hit count or lower bound.
		Total Total `json:"total"`
		// Hits contains this page of documents in response order.
		Hits []Hit[T] `json:"hits"`
		// MaxScore is nil when scores are not available.
		MaxScore *float64 `json:"max_score"`
	} `json:"hits"`
	// Aggregations contains raw JSON keyed by aggregation name, or type#name when typed_keys is
	// enabled.
	Aggregations map[string]json.RawMessage `json:"aggregations,omitempty"`
	// Suggest contains raw named suggestion responses; nil means none were returned.
	Suggest map[string]json.RawMessage `json:"suggest,omitempty"`
	// Shards reports searched shards and failures. A nonzero failed count yields PartialSearchError.
	Shards struct {
		// Total is the number of shards participating in the search.
		Total int `json:"total"`
		// Failed is the count of unsuccessful shards.
		Failed int `json:"failed"`
		// Failures holds raw shard errors, which can contain sensitive data.
		Failures []json.RawMessage `json:"failures,omitempty"`
	} `json:"_shards"`
}

// PartialSearchError reports a timeout or failed shards in a search-like response.
type PartialSearchError struct {
	// TimedOut is true when the server reported a search timeout.
	TimedOut bool
	// FailedShards is the number of failed shards reported by the server.
	FailedShards int
}

// Error returns a human-readable description of the failure.
func (e *PartialSearchError) Error() string {
	return fmt.Sprintf("esodm: incomplete search (timed_out=%t failed_shards=%d)", e.TimedOut, e.FailedShards)
}

// Search is an immutable snapshot of a search body, safe for concurrent reuse.
type Search struct {
	fields map[string]json.RawMessage
	params url.Values
	err    error
}

// SearchFromRequest snapshots an official search request, preserving explicitly
// supplied options. It adds ODM metadata defaults only when they are absent.
func SearchFromRequest(request *typedsearch.Request) Search {
	if request == nil {
		return Search{err: fmt.Errorf("%w: nil search request", ErrValidation)}
	}
	data, err := json.Marshal(request)
	if err != nil {
		return Search{err: err}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return Search{err: err}
	}
	s := Search{fields: fields}
	for _, key := range []string{"track_total_hits", "seq_no_primary_term"} {
		if _, exists := fields[key]; !exists {
			s = s.With(key, true)
		}
	}
	return s
}

// NewSearch creates a search with exact hit counts and concurrency metadata enabled.
func NewSearch(q Query) Search {
	return Search{}.With("query", q).With("track_total_hits", true).With("seq_no_primary_term", true)
}

// With is an immutable escape hatch for additional search body options.
func (s Search) With(key string, value any) Search {
	if s.err != nil {
		return s
	}
	if key == "" {
		s.err = fmt.Errorf("%w: empty search option", ErrValidation)
		return s
	}
	fields := maps.Clone(s.fields)
	if fields == nil {
		fields = make(map[string]json.RawMessage)
	}
	b, err := json.Marshal(value)
	fields[key] = b
	return Search{fields: fields, params: s.params, err: err}
}

// MarshalJSON serializes the snapshot, returning any deferred construction error.
func (s Search) MarshalJSON() ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.fields == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(s.fields)
}

// Size sets the maximum hits per page; negative values defer a validation error.
func (s Search) Size(n int) Search {
	if n < 0 {
		s.err = fmt.Errorf("%w: negative size", ErrValidation)
		return s
	}
	return s.With("size", n)
}

// From sets the result offset; use PIT iteration for deep pagination.
func (s Search) From(n int) Search {
	if n < 0 {
		s.err = fmt.Errorf("%w: negative offset", ErrValidation)
		return s
	}
	return s.With("from", n)
}

// Sort describes an ascending or descending field sort.
type Sort struct {
	// Field is the required Elasticsearch field name or supported special sort such as _score or
	// _shard_doc.
	Field string
	// Desc selects descending order when true; false selects ascending order.
	Desc bool
}

// Sort sets the ordered sort keys, rejecting empty field names.
func (s Search) Sort(fields ...Sort) Search {
	v := make([]map[string]string, len(fields))
	for i, f := range fields {
		if f.Field == "" {
			s.err = fmt.Errorf("%w: empty sort field", ErrValidation)
			return s
		}
		order := "asc"
		if f.Desc {
			order = "desc"
		}
		v[i] = map[string]string{f.Field: order}
	}
	return s.With("sort", v)
}

// Source selects source fields to include or exclude. Partial sources must not
// be used for a full-document replacement without restoring omitted fields.
func (s Search) Source(includes, excludes []string) Search {
	return s.With("_source", types.SourceFilter{Includes: includes, Excludes: excludes})
}

// Highlight requests default highlighting for the supplied fields.
func (s Search) Highlight(fields ...string) Search {
	m := make(map[string]types.HighlightField, len(fields))
	for _, f := range fields {
		m[f] = types.HighlightField{}
	}
	return s.With("highlight", types.Highlight{Fields: []map[string]types.HighlightField{m}})
}

// Collapse groups returned hits by a field value.
func (s Search) Collapse(field string) Search {
	return s.With("collapse", types.FieldCollapse{Field: field})
}

// Aggregations sets named aggregation snapshots on the search.
func (s Search) Aggregations(a map[string]Aggregation) Search {
	return s.With("aggs", a)
}

// Runtime sets dynamic runtime field definitions; official runtime types can
// also be supplied through SearchFromRequest or With.
func (s Search) Runtime(fields map[string]any) Search {
	return s.With("runtime_mappings", fields)
}

// Suggest sets a named suggester of kind using the supplied field and text.
// Deprecated: Use SuggestWith with the official FieldSuggester type.
func (s Search) Suggest(name, field, text, kind string) Search {
	return s.With("suggest", map[string]any{name: map[string]any{"text": text, kind: map[string]any{"field": field}}})
}

// SearchAfter sets the last hit sort values without converting JSON numbers.
func (s Search) SearchAfter(values ...json.RawMessage) Search {
	return s.With("search_after", values)
}

// PIT selects an existing point-in-time ID and renews its keep-alive duration.
func (s Search) PIT(id, keepAlive string) Search {
	return s.With("pit", types.PointInTimeReference{Id: id, KeepAlive: keepAlive})
}

// KNN describes a vector search with an optional filter. K must be positive
// and Candidates must be between K and 10,000.
type KNN struct {
	// Field is a required dense_vector field name.
	Field string `json:"field"`
	// Vector is a nonempty query vector of finite values. Its dimension and similarity constraints
	// must match the server mapping.
	Vector []float32 `json:"query_vector"`
	// K is the positive requested nearest-neighbor count; it must not exceed Candidates.
	K int `json:"k"`
	// Candidates is the per-shard candidate count, K..10000; zero is invalid.
	Candidates int `json:"num_candidates"`
	// Filter optionally restricts candidate documents; nil applies no additional filter. Search.KNN
	// snapshots it.
	Filter *Query `json:"filter,omitempty"`
}

// KNN sets a validated vector search using the official request model.
func (s Search) KNN(k KNN) Search {
	if k.Field == "" || len(k.Vector) == 0 || k.K < 1 || k.Candidates < k.K || k.Candidates > 10000 {
		s.err = fmt.Errorf("%w: invalid kNN parameters", ErrValidation)
		return s
	}
	value := types.KnnSearch{Field: k.Field, QueryVector: k.Vector, K: &k.K, NumCandidates: &k.Candidates}
	if k.Filter != nil {
		filter, err := k.Filter.typed()
		if err != nil {
			s.err = err
			return s
		}
		value.Filter = []types.Query{*filter}
	}
	return s.With("knn", value)
}

// SparseVector creates a query from precomputed sparse-vector tokens.
func SparseVector(field string, tokens map[string]float32) Query {
	return FromQuery(&types.Query{SparseVector: esdsl.NewSparseVectorQuery().Field(field).QueryVector(tokens).SparseVectorQueryCaster()})
}

// HybridRRF uses Elasticsearch's RRF retriever, which requires an appropriate
// server license. License errors are preserved rather than silently downgraded.
func (s Search) HybridRRF(lexical Query, vector KNN, window, constant int) Search {
	if window < 1 || constant < 1 {
		s.err = fmt.Errorf("%w: invalid RRF parameters", ErrValidation)
		return s
	}
	checked := Search{}.KNN(vector)
	if checked.err != nil {
		s.err = checked.err
		return s
	}
	s = s.without("query", "knn")
	lexicalQuery, err := lexical.typed()
	if err != nil {
		s.err = err
		return s
	}
	knn := types.KnnRetriever{Field: vector.Field, QueryVector: vector.Vector, K: vector.K, NumCandidates: &vector.Candidates}
	if vector.Filter != nil {
		filter, err := vector.Filter.typed()
		if err != nil {
			s.err = err
			return s
		}
		knn.Filter = []types.Query{*filter}
	}
	return s.With("retriever", types.RetrieverContainer{Rrf: &types.RRFRetriever{
		RankWindowSize: &window, RankConstant: &constant,
		Retrievers: []types.RRFRetrieverEntry{
			types.RetrieverContainer{Standard: &types.StandardRetriever{Query: lexicalQuery}},
			types.RetrieverContainer{Knn: &knn},
		},
	}})
}
func (s Search) without(keys ...string) Search {
	m := maps.Clone(s.fields)
	for _, k := range keys {
		delete(m, k)
	}
	s.fields = m
	return s
}

// Search executes a typed document search and runs AfterRead for returned hits.
// It returns PartialSearchError when the server reports incomplete results.
func (r *Repository[T]) Search(ctx context.Context, search Search) (SearchResult[T], error) {
	search = r.includeSourceVectors(search)
	var out SearchResult[T]
	if err := search.validatePITParams(); err != nil {
		return out, err
	}
	body, err := json.Marshal(search)
	if err != nil {
		return out, err
	}
	request := typedsearch.New(nil).Raw(bytes.NewReader(body)).AllowPartialSearchResults(false).
		Header("Accept", "application/json").Header("Content-Type", "application/json")
	if _, hasPIT := search.fields["pit"]; !hasPIT {
		request.Index(r.schema.index)
	}
	err = r.client.DoTyped(ctx, parameterizedRequest{request, search.params}, &out)
	if err == nil && (out.TimedOut || out.Shards.Failed > 0) {
		err = &PartialSearchError{out.TimedOut, out.Shards.Failed}
	}
	if err == nil {
		for i := range out.Hits.Hits {
			if err = r.afterRead(ctx, &out.Hits.Hits[i]); err != nil {
				break
			}
		}
	}
	return out, err
}

// MultiSearchResult pairs an individual result with its item-level error.
type MultiSearchResult[T any] struct {
	// Result contains the search response, possibly partial when Err is non-nil.
	Result SearchResult[T]
	// Err is this search response error; the outer MSearch error instead reports a whole-request
	// failure.
	Err error
}

// MultiSearch executes searches in one request and preserves their order.
// Request errors are returned separately from individual search errors.
func (r *Repository[T]) MultiSearch(ctx context.Context, searches ...Search) ([]MultiSearchResult[T], error) {
	if len(searches) == 0 {
		return []MultiSearchResult[T]{}, nil
	}
	var body []byte
	params := url.Values{}
	typedKeys := searches[0].params.Get("typed_keys")
	for _, s := range searches {
		if err := s.validatePITParams(); err != nil {
			return nil, err
		}
		if s.params.Get("typed_keys") != typedKeys {
			return nil, fmt.Errorf("%w: msearch requires the same typed_keys setting for all searches", ErrValidation)
		}
		s = r.includeSourceVectors(s)
		header := map[string]any{"index": r.schema.index, "allow_partial_search_results": false}
		if _, ok := s.fields["pit"]; ok {
			delete(header, "index")
		}
		for key, values := range s.params {
			value := values[0]
			switch key {
			case "typed_keys":
				params.Set(key, value)
			case "routing", "preference", "expand_wildcards":
				header[key] = value
			case "request_cache", "ignore_unavailable", "allow_no_indices":
				parsed, err := strconv.ParseBool(value)
				if err != nil {
					return nil, fmt.Errorf("%w: %w", ErrValidation, err)
				}
				header[key] = parsed
			default:
				return nil, fmt.Errorf("%w: msearch parameter %s has no per-search equivalent", ErrUnsupported, key)
			}
		}
		h, _ := json.Marshal(header)
		b, err := json.Marshal(s)
		if err != nil {
			return nil, err
		}
		body = append(body, h...)
		body = append(body, '\n')
		body = append(body, b...)
		body = append(body, '\n')
	}
	var envelope struct {
		Responses []json.RawMessage `json:"responses"`
	}
	if err := r.client.request(ctx, "POST", "/_msearch", params, body, "application/x-ndjson", &envelope); err != nil {
		return nil, err
	}
	if len(envelope.Responses) != len(searches) {
		return nil, fmt.Errorf("esodm: msearch response count mismatch")
	}
	out := make([]MultiSearchResult[T], len(searches))
	for i, raw := range envelope.Responses {
		var e struct {
			Status int               `json:"status"`
			Error  *types.ErrorCause `json:"error"`
		}
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		if e.Error != nil {
			out[i].Err = &Error{Status: e.Status, Type: e.Error.Type, Reason: errorReason(e.Error), Cause: *e.Error, Body: raw}
			continue
		}
		out[i].Err = json.Unmarshal(raw, &out[i].Result)
		if out[i].Err == nil && (out[i].Result.TimedOut || out[i].Result.Shards.Failed > 0) {
			out[i].Err = &PartialSearchError{out[i].Result.TimedOut, out[i].Result.Shards.Failed}
		}
		if out[i].Err == nil {
			for j := range out[i].Result.Hits.Hits {
				if out[i].Err = r.afterRead(ctx, &out[i].Result.Hits.Hits[j]); out[i].Err != nil {
					break
				}
			}
		}
	}
	return out, nil
}

// ES 9 excludes vectors by default. Hydrate them for full-document reads so a
// read/replace cycle cannot silently erase them. Explicit source filtering wins.
func (r *Repository[T]) includeSourceVectors(s Search) Search {
	if r.client.version.Major != 9 || !r.schema.hasVectors {
		return s
	}
	raw, ok := s.fields["_source"]
	if !ok || string(raw) == "true" {
		return s.With("_source", map[string]bool{"exclude_vectors": false})
	}
	var source map[string]json.RawMessage
	if json.Unmarshal(raw, &source) == nil && source != nil {
		if _, explicit := source["exclude_vectors"]; !explicit {
			source["exclude_vectors"] = json.RawMessage("false")
			return s.With("_source", source)
		}
	}
	return s
}

// Param returns a copy with an additional search URL parameter. Metadata-hiding
// filter_path and partial-result overrides are rejected by the ODM contract.
func (s Search) Param(key, value string) Search {
	if s.err != nil {
		return s
	}
	if key == "" || key == "filter_path" || key == "allow_partial_search_results" || strings.ContainsAny(key, "\r\n") {
		s.err = fmt.Errorf("%w: invalid or reserved search parameter %q", ErrValidation, key)
		return s
	}
	s.params = s.Params()
	s.params.Set(key, value)
	return s
}

// Params returns an independent copy of the URL parameters.
func (s Search) Params() url.Values {
	copy := make(url.Values, len(s.params))
	for k, v := range s.params {
		copy[k] = append([]string(nil), v...)
	}
	return copy
}

// Routing targets shards for the supplied routing values; it cannot be used with a PIT.
func (s Search) Routing(values ...string) Search {
	return s.Param("routing", strings.Join(values, ","))
}

// Preference selects the shard or session preference; it cannot be used with a PIT.
func (s Search) Preference(value string) Search { return s.Param("preference", value) }

// RequestCache enables or disables Elasticsearch's request cache for this search.
func (s Search) RequestCache(enabled bool) Search {
	return s.Param("request_cache", strconv.FormatBool(enabled))
}

// TypedKeys requests aggregation type prefixes, enabling Aggregates decoding.
func (s Search) TypedKeys(enabled bool) Search {
	return s.Param("typed_keys", strconv.FormatBool(enabled))
}

// Timeout sets the server search timeout, rounded up to whole milliseconds.
func (s Search) Timeout(duration time.Duration) Search {
	if duration <= 0 {
		s.err = fmt.Errorf("%w: timeout must be positive", ErrValidation)
		return s
	}
	milliseconds := duration / time.Millisecond
	if duration%time.Millisecond != 0 {
		milliseconds++
	}
	return s.With("timeout", strconv.FormatInt(int64(milliseconds), 10)+"ms")
}

// MinScore excludes hits whose score is lower than score.
func (s Search) MinScore(score float64) Search { return s.With("min_score", score) }

// SortBy accepts the complete official sort model, including nested and missing options.
func (s Search) SortBy(sorts ...types.SortCombinations) Search { return s.With("sort", sorts) }

// SuggestWith sets an official typed suggester without positional option strings.
func (s Search) SuggestWith(name string, suggester types.FieldSuggester) Search {
	return s.With("suggest", map[string]types.FieldSuggester{name: suggester})
}

// Err returns a deferred construction error without executing a request.
func (s Search) Err() error { return s.err }

// Sources returns source values in hit order. Maps and pointers within T are shared with the hits.
func (r SearchResult[T]) Sources() []T {
	out := make([]T, len(r.Hits.Hits))
	for i, hit := range r.Hits.Hits {
		out[i] = hit.Source
	}
	return out
}

// Total returns the server hit count; consult Exact to distinguish lower bounds.
func (r SearchResult[T]) Total() int64 { return r.Hits.Total.Value }

// Exact reports whether Total is an exact count.
func (r SearchResult[T]) Exact() bool { return r.Hits.Total.Relation == "eq" }

// Len returns the number of hits in this page.
func (r SearchResult[T]) Len() int { return len(r.Hits.Hits) }

func (s Search) validatePITParams() error {
	if s.err != nil {
		return s.err
	}
	if _, ok := s.fields["pit"]; ok {
		for _, key := range []string{"routing", "preference"} {
			if _, ok := s.params[key]; ok {
				return fmt.Errorf("%w: PIT search cannot specify %s", ErrValidation, key)
			}
		}
	}
	return nil
}

type parameterizedRequest struct {
	RequestBuilder
	params url.Values
}

func (p parameterizedRequest) HttpRequest(ctx context.Context) (*http.Request, error) { //nolint:staticcheck // Match the official RequestBuilder interface.
	req, err := p.RequestBuilder.HttpRequest(ctx)
	if err != nil {
		return req, err
	}
	query := req.URL.Query()
	for key, values := range p.params {
		query[key] = append([]string(nil), values...)
	}
	req.URL.RawQuery = query.Encode()
	return req, nil
}

// Aggregates decodes typed_keys aggregation results using the official response decoder.
// Unknown aggregation types and ambiguous normalized names are rejected.
func (r SearchResult[T]) Aggregates() (map[string]types.Aggregate, error) {
	names := map[string]bool{}
	for key := range r.Aggregations {
		kind, name, ok := strings.Cut(key, "#")
		if !ok || kind == "" || name == "" || strings.Contains(name, "#") || names[name] {
			return nil, fmt.Errorf("%w: aggregation keys require unique type#name values", ErrValidation)
		}
		names[name] = true
	}
	data, err := json.Marshal(map[string]any{"aggregations": r.Aggregations})
	if err != nil {
		return nil, err
	}
	var response typedsearch.Response
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	if len(response.Aggregations) != len(names) {
		return nil, fmt.Errorf("%w: unknown aggregation type", ErrUnsupported)
	}
	return response.Aggregations, nil
}

// InnerHits decodes one named inner-hit group as U. Source documents are decoded
// without repository hooks or metadata hydration because no U repository is bound.
// An absent group returns ErrNotFound; malformed JSON returns a decoding error.
func InnerHits[U any, T any](hit Hit[T], name string) (SearchResult[U], error) {
	var out SearchResult[U]
	raw, ok := hit.InnerHits[name]
	if !ok {
		return out, ErrNotFound
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	if out.TimedOut || out.Shards.Failed > 0 {
		return out, &PartialSearchError{TimedOut: out.TimedOut, FailedShards: out.Shards.Failed}
	}
	return out, nil
}
