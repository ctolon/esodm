# Aggregations and facets

## Aggregations

`Search.Aggregations` and `Finder.Aggregate` take named `Aggregation` values:

- `FromAggregation(builder)` snapshots any official `esdsl` aggregation builder.
- `Avg`, `Sum`, `Min`, `Max`, `Stats`, `Cardinality` and `ValueCount` create field metrics.
- `TermsAgg(field, size, children)` creates a terms bucket aggregation with optional sub-aggregations.
- `Agg(kind, params, children)` and `PipelineAgg(kind, paths, script)` build an aggregation from raw parameters.

Results are available in three forms:

- `result.Aggregations[name]` is the raw JSON.
- `DecodeAgg[T](result.Aggregations, name)` decodes one aggregation into an application-defined type such as `CategoryBucket` below.
- `result.Aggregates()` decodes every aggregation into the official `types.Aggregate` union. It requires the search to have been run with `TypedKeys(true)`, which makes the server prefix each name with its type.

## Disjunctive facets

`WithFacets` builds the search shape used by faceted navigation: every selected facet filters the hits, while each facet's own counts are computed with all other selections applied but its own selection omitted. Selecting "books" therefore still shows the counts of the other categories.

<!-- source: examples/guide/facets.go -->
```go
package guide

import (
	"context"

	"github.com/ctolon/esodm"
)

// CategoryBucket decodes the part of a terms aggregation used by the application.
type CategoryBucket struct {
	Buckets []struct {
		Key   string `json:"key"`
		Count int64  `json:"doc_count"`
	} `json:"buckets"`
}

// FacetBooks counts categories while keeping the selected price filter.
func FacetBooks(ctx context.Context, repo *esodm.Repository[Product]) (CategoryBucket, error) {
	search := esodm.WithFacets(esodm.NewSearch(esodm.MatchAll()), map[string]esodm.Facet{
		"categories": {Aggregation: esodm.TermsAgg("category", 20, nil), Selection: esodm.Term("category", "books")},
		"prices":     {Aggregation: esodm.Avg("price"), Selection: esodm.OrderedValue[int]("price").LTE(30)},
	})
	result, err := repo.Search(ctx, search.TypedKeys(true))
	if err != nil {
		return CategoryBucket{}, err
	}
	return esodm.DecodeFacet[CategoryBucket](result.Aggregations, "categories")
}
```

Each `Facet` pairs an aggregation with an optional `Selection` query; combine several selected values with `Terms` or `Or`. `DecodeFacet[T]` unwraps the facet's inner aggregation and accepts both plain and typed-key responses. `WithFacets` keeps aggregations already on the search and rejects a facet name that collides with one, or a search that already has a `post_filter`.

Bucket order, approximate counts and the size limit of a terms aggregation keep their Elasticsearch meaning.

## Optional children and raw parameters

`TermsAgg` takes an explicit bucket size and an optional child map; nil children omit sub-aggregations. Use a positive size within the cluster's bucket limits. The helper transmits the supplied size without choosing a zero default. `Agg` requires a nonempty aggregation kind and snapshots JSON-serializable parameters; nil parameters serialize as null, so pass the object required by the selected aggregation. Unknown kinds are deliberately left for server validation.

`PipelineAgg` is an escape hatch: `paths` supplies the Elasticsearch buckets_path shape (a string, list or named map as supported by the chosen pipeline kind), and `script` is the script source. Both are serialized as supplied. Prefer official builders when a pipeline has no script or needs additional settings. `MetricAgg` and `Search.Suggest` are deprecated in favor of named metrics and official suggester types.

`DecodeAgg` accepts exact names and falls back to matching the suffix of a typed key. Multiple matching typed suffixes return ErrValidation; an absent name returns ErrNotFound; an incompatible destination type returns a JSON decoding error. Numeric metric values can be null for empty buckets, so use pointer or nullable fields in application result types when null must be distinguished from zero. `Aggregates` requires unique, supported type#name keys and rejects ambiguous normalized names.

Facet selections are optional: the zero Query selects nothing extra. An empty facet map preserves the base search, except that an existing post_filter remains an invalid combination. Facet names must be nonempty and distinct from existing aggregation names; a zero aggregation is rejected. Source filtering and requested hit size do not restrict the documents counted by an aggregation.
