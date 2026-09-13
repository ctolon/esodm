package esodm

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"

	"github.com/elastic/go-elasticsearch/v9/typedapi/esdsl"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

// Finder is an immutable repository-bound search. Copies can be reused concurrently.
// All returns one search window; Each traverses all matching documents using a PIT.
type Finder[T any] struct {
	repo                          *Repository[T]
	base                          Search
	must, filter, should, mustNot []Query
}

// Find starts a search requiring all supplied queries. No queries matches everything.
func (r *Repository[T]) Find(q ...Query) Finder[T] {
	return Finder[T]{repo: r, base: NewSearch(MatchAll()).without("query"), must: slices.Clone(q)}
}

// FindSearch continues an existing immutable search, retaining its options and query.
func (r *Repository[T]) FindSearch(s Search) Finder[T] { return Finder[T]{repo: r, base: s} }

// Must adds required scoring clauses.
func (f Finder[T]) Must(q ...Query) Finder[T] { f.must = append(slices.Clone(f.must), q...); return f }

// Filter adds required clauses without score contributions.
func (f Finder[T]) Filter(q ...Query) Finder[T] {
	f.filter = append(slices.Clone(f.filter), q...)
	return f
}

// Should adds optional scoring clauses. Without required clauses, at least one must match.
func (f Finder[T]) Should(q ...Query) Finder[T] {
	f.should = append(slices.Clone(f.should), q...)
	return f
}

// Not excludes documents matching any supplied clause.
func (f Finder[T]) Not(q ...Query) Finder[T] {
	f.mustNot = append(slices.Clone(f.mustNot), q...)
	return f
}

func (f Finder[T]) compile() Query {
	must := slices.Clone(f.must)
	if raw, ok := f.base.fields["query"]; ok {
		must = append([]Query{RawQuery(raw)}, must...)
	}
	if len(f.filter)+len(f.should)+len(f.mustNot) == 0 {
		if len(must) == 0 {
			return MatchAll()
		}
		if len(must) == 1 {
			return must[0]
		}
	}
	builder := esdsl.NewBoolQuery()
	for i, list := range [][]Query{must, f.filter, f.should, f.mustNot} {
		clauses := make([]types.QueryVariant, len(list))
		for j, q := range list {
			v, err := q.typed()
			if err != nil {
				return Query{err: err}
			}
			clauses[j] = v
		}
		if len(clauses) == 0 {
			continue
		}
		switch i {
		case 0:
			builder.Must(clauses...)
		case 1:
			builder.Filter(clauses...)
		case 2:
			builder.Should(clauses...)
		case 3:
			builder.MustNot(clauses...)
		}
	}
	if len(f.should) > 0 && len(must)+len(f.filter) == 0 {
		builder.MinimumShouldMatch(esdsl.NewMinimumShouldMatch().Int(1))
	}
	return FromQuery(builder)
}

// Search returns the compiled snapshot for use with other repository operations.
func (f Finder[T]) Search() Search {
	if len(f.must)+len(f.filter)+len(f.should)+len(f.mustNot) == 0 && f.base.fields["query"] == nil && (f.base.fields["knn"] != nil || f.base.fields["retriever"] != nil) {
		return f.base
	}
	return f.base.With("query", f.compile())
}

// Err returns construction errors, including an unbound repository.
func (f Finder[T]) Err() error {
	if f.repo == nil {
		return fmt.Errorf("%w: unbound finder", ErrValidation)
	}
	return f.Search().Err()
}

// Size sets the maximum number of hits in a search window.
func (f Finder[T]) Size(n int) Finder[T] { f.base = f.base.Size(n); return f }

// From sets the search offset.
func (f Finder[T]) From(n int) Finder[T] { f.base = f.base.From(n); return f }

// Sort replaces the search sort order.
func (f Finder[T]) Sort(s ...Sort) Finder[T] { f.base = f.base.Sort(s...); return f }

// Source selects source fields to include and exclude.
func (f Finder[T]) Source(includes, excludes []string) Finder[T] {
	f.base = f.base.Source(includes, excludes)
	return f
}

// Highlight selects fields for highlighting.
func (f Finder[T]) Highlight(fields ...string) Finder[T] {
	f.base = f.base.Highlight(fields...)
	return f
}

// Collapse groups hits by a field.
func (f Finder[T]) Collapse(field string) Finder[T] { f.base = f.base.Collapse(field); return f }

// Aggregate adds or replaces a named aggregation, retaining existing aggregations.
func (f Finder[T]) Aggregate(name string, a Aggregation) Finder[T] {
	if f.base.err != nil {
		return f
	}
	if name == "" {
		f.base.err = fmt.Errorf("%w: empty aggregation name", ErrValidation)
		return f
	}
	var aggs map[string]json.RawMessage
	raw := f.base.fields["aggs"]
	if raw == nil {
		raw = f.base.fields["aggregations"]
	}
	if raw != nil {
		if err := json.Unmarshal(raw, &aggs); err != nil {
			f.base.err = err
			return f
		}
	}
	aggs = maps.Clone(aggs)
	if aggs == nil {
		aggs = make(map[string]json.RawMessage)
	}
	data, err := a.MarshalJSON()
	if err != nil {
		f.base.err = err
		return f
	}
	aggs[name] = data
	f.base = f.base.without("aggregations").With("aggs", aggs)
	return f
}

// KNN configures a nearest-neighbor search.
func (f Finder[T]) KNN(k KNN) Finder[T] { f.base = f.base.KNN(k); return f }

// With snapshots an additional search body option.
func (f Finder[T]) With(key string, value any) Finder[T] { f.base = f.base.With(key, value); return f }

// Routing limits the search to the supplied routing values.
func (f Finder[T]) Routing(values ...string) Finder[T] { f.base = f.base.Routing(values...); return f }

// Preference sets shard preference.
func (f Finder[T]) Preference(p string) Finder[T] { f.base = f.base.Preference(p); return f }

// Result executes the search, preserving partial results and their error.
func (f Finder[T]) Result(ctx context.Context) (SearchResult[T], error) {
	if err := f.Err(); err != nil {
		return SearchResult[T]{}, err
	}
	return f.repo.Search(ctx, f.Search())
}

// All returns hits from one search window, preserving partial results on failure.
func (f Finder[T]) All(ctx context.Context) ([]Hit[T], error) {
	r, err := f.Result(ctx)
	return r.Hits.Hits, err
}

// Sources returns source documents from one search window.
func (f Finder[T]) Sources(ctx context.Context) ([]T, error) {
	r, err := f.Result(ctx)
	return r.Sources(), err
}

// First returns the first hit at the configured offset, overriding Size with one.
// A successful empty search returns ErrNotFound.
func (f Finder[T]) First(ctx context.Context) (Hit[T], error) {
	hits, err := f.Size(1).All(ctx)
	if len(hits) > 0 {
		return hits[0], err
	}
	if err != nil {
		return Hit[T]{}, err
	}
	return Hit[T]{}, ErrNotFound
}

// Count counts query matches, ignoring pagination, sorting, aggregations and KNN.
// Routing and preference are preserved.
func (f Finder[T]) Count(ctx context.Context) (int64, error) {
	if err := f.Err(); err != nil {
		return 0, err
	}
	p := f.base.Params()
	var routing []string
	if v := p.Get("routing"); v != "" {
		routing = strings.Split(v, ",")
	}
	return f.repo.CountWith(ctx, f.compile(), CountOptions{Routing: routing, Preference: p.Get("preference")})
}

// Exists reports whether the configured query matches a document. It ignores the
// offset and size, requests one hit and stops collecting after one match per shard.
func (f Finder[T]) Exists(ctx context.Context) (bool, error) {
	f.base = f.base.without("from", "search_after").Size(1).With("track_total_hits", false).With("terminate_after", 1)
	if f.base.fields["post_filter"] != nil {
		f.base = f.base.without("terminate_after")
	}
	r, err := f.Result(ctx)
	return r.Len() > 0, err
}

// Page is a one-based search window with an exact or lower-bound document total.
// HasNext compares the returned window with that total; it is conservative when Exact is false.
type Page[T any] struct {
	// Hits contains the requested page in server sort order.
	Hits []Hit[T]
	// Number is the one-based requested page number.
	Number int
	// Size is the requested page size.
	Size int
	// Total is the reported hit count; Exact indicates whether it is a lower bound.
	Total int64
	// Exact is true only when Elasticsearch reports an exact total.
	Exact bool
	// HasNext indicates more results according to the returned count or lower bound; it is not a
	// snapshot guarantee.
	HasNext bool
}

// Sources returns the source documents in this page.
func (p Page[T]) Sources() []T {
	out := make([]T, len(p.Hits))
	for i, h := range p.Hits {
		out[i] = h.Source
	}
	return out
}

// Page retrieves a numbered window. Deep pagination is limited by the index's
// max_result_window; use Each for traversal beyond that limit.
func (f Finder[T]) Page(ctx context.Context, page, size int) (Page[T], error) {
	maxInt := int(^uint(0) >> 1)
	if page < 1 || size < 1 || page > maxInt/size {
		return Page[T]{}, fmt.Errorf("%w: invalid page or size", ErrValidation)
	}
	from := (page - 1) * size
	r, err := f.From(from).Size(size).Result(ctx)
	return Page[T]{Hits: r.Hits.Hits, Number: page, Size: size, Total: r.Total(), Exact: r.Exact(), HasNext: int64(from)+int64(r.Len()) < r.Total()}, err
}

// Each traverses matching hits using a PIT with a one-minute keep-alive.
// Stopping iteration closes the PIT; cleanup failures can only be yielded while the consumer continues.
func (f Finder[T]) Each(ctx context.Context, pageSize int) iter.Seq2[Hit[T], error] {
	return func(yield func(Hit[T], error) bool) {
		if err := f.Err(); err != nil {
			yield(Hit[T]{}, err)
			return
		}
		f.repo.Each(ctx, f.Search(), pageSize, "1m")(yield)
	}
}

// EachSource traverses matching source documents, closing the PIT on termination.
func (f Finder[T]) EachSource(ctx context.Context, pageSize int) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for h, err := range f.Each(ctx, pageSize) {
			if !yield(h.Source, err) {
				return
			}
		}
	}
}
