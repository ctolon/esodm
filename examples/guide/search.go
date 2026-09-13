package guide

import (
	"context"
	"time"

	"github.com/ctolon/esodm"
	"github.com/elastic/go-elasticsearch/v9/typedapi/esdsl"
)

// FindBooks returns a sorted page with highlighting and aggregation results.
func FindBooks(ctx context.Context, repo *esodm.Repository[Product]) (esodm.SearchResult[Product], error) {
	query := esodm.And(
		esodm.FromQuery(esdsl.NewMatchQuery("name", "Go")),
		esodm.Filter(esodm.Term("category", "books"), esodm.OrderedValue[int]("price").LTE(30)),
	)
	search := esodm.NewSearch(query).Size(20).
		Sort(esodm.OrderedValue[int]("price").Asc()).
		Highlight("name").Timeout(2 * time.Second).
		Aggregations(map[string]esodm.Aggregation{
			"average_price": esodm.FromAggregation(esdsl.NewAverageAggregation().Field("price")),
		})
	return repo.Search(ctx, search)
}
