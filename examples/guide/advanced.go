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
