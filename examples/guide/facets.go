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
