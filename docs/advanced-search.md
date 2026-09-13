# Advanced search

<!-- source: examples/guide/advanced.go -->
```go
package guide

import (
	"encoding/json"

	"github.com/ctolon/esodm"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

// NearbyReviews combines independent nested and geographic filters.
func NearbyReviews() esodm.Search {
	return esodm.NewSearch(esodm.Filter(
		esodm.Nested("reviews", esodm.OrderedValue[int]("reviews.rating").GTE(4)),
		esodm.Geo("location").WithinDistance("5km", esodm.GeoPoint{Lat: 41, Lon: 29}),
	))
}

// MatchSavedQueries tests a document against a percolator-mapped query field.
func MatchSavedQueries(document json.RawMessage) esodm.Search {
	return esodm.NewSearch(esodm.Percolate(types.PercolateQuery{Field: "query", Document: document}))
}

// Hybrid combines lexical retrieval and a precomputed dense embedding.
func Hybrid(vector []float32) esodm.Search {
	lexical := esodm.Text("name").Match("search systems")
	return esodm.NewSearch(lexical).Size(10).HybridRRF(lexical,
		esodm.KNN{Field: "embedding", Vector: vector, K: 20, Candidates: 100}, 50, 60)
}
```

## Nested and geo queries

`Nested(path, query)` wraps a query for a nested field; field names inside it include the nested path. Generated `ObjectField` descriptors provide `Nested` and child fields with the full path.

`GeoPoint` is the official latitude/longitude type and maps to `geo_point`. `Geo(name)` returns a descriptor with `WithinDistance` and `WithinBox`. `GeoShape(types.GeoShapeQuery)` snapshots a geo-shape query for a field mapped as `geo_shape`.

## Percolate

Map a `percolator` field and the document fields that stored queries reference, then index the queries as documents. `Percolate(types.PercolateQuery)` matches a candidate document against them and returns the stored queries that match. Exactly one of `Document`, `Documents` or a stored document `Id` must be set.

## Dense, sparse and hybrid retrieval

Map dense vectors with `dims` and `similarity` through tags, `SchemaConfig.Properties` or `SchemaFromMapping`. `Search.KNN` takes a `KNN` value with the field, query vector, `K` and `Candidates` (between `K` and 10,000) and an optional filter. `SparseVector(field, tokens)` queries precomputed token weights; omit an absent sparse field rather than writing JSON null.

`HybridRRF(lexical, knn, window, constant)` combines a lexical query and a vector search with the RRF retriever. RRF requires a license that includes it; the server's license error is returned unchanged. The `Hybrid` example expects an `embedding` field mapped with the same dimensions as the query vector.

Embeddings are produced outside the ODM, either by the application or by an ingest pipeline.

## Semantic text

`Semantic(field, text)` queries a `semantic_text` field. Map the field with `es:"type=semantic_text,inference_id=my-endpoint"` or with the official property types through `SchemaFromMapping`. The inference endpoint is provisioned on the cluster.

## Inner hits

`InnerHits[Child](hit, "reviews")` decodes the named inner-hit group of a nested or join query into `SearchResult[Child]`. A missing group returns `ErrNotFound`. Inner hits are decoded without repository hooks or metadata hydration for the child type.

## Parameter contracts

`HybridRRF` requires positive `window` and `constant` values; neither has a zero default. The server also validates the rank window against the requested hit size. The method replaces existing top-level query and knn clauses with a retriever; it preserves other search settings. A KNN filter is optional, but a supplied filter must be a valid Query. Dense query vectors must be nonempty and finite, and match the configured dimension and similarity constraints.

`SparseVector` accepts a map of precomputed token names to float32 weights; the JSON snapshot rejects NaN/infinities and the server validates token/weight constraints. `Semantic` expects a semantic_text field and query text; the endpoint's availability, model downloads, quotas, license and credentials are external prerequisites. The ODM does not generate embeddings or silently substitute lexical search after inference failures.

`GeoPoint{Lat, Lon}` uses latitude then longitude as named fields; GeoJSON coordinates instead use longitude then latitude. Use valid latitude/longitude ranges for the selected server geometry type. `WithinDistance` requires a nonempty Elasticsearch distance such as `5km`, `500m` or `2mi` and a non-nil official GeoLocation value; it is not a Go duration. `WithinBox` accepts official bounding-box alternatives. Advanced geometry validation is performed by Elasticsearch.

The wrappers preserve official request types so that users can supply newer supported options without waiting for a new convenience helper. Consult the server's [query DSL reference](https://www.elastic.co/docs/reference/query-languages/querydsl) and [mapping reference](https://www.elastic.co/docs/reference/elasticsearch/mapping-reference) for those open-ended options.
