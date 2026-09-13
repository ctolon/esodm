package guide

import (
	"context"

	"github.com/ctolon/esodm"
)

// FindAffordableBooks returns one search window while retaining hit metadata.
func FindAffordableBooks(ctx context.Context, products *esodm.Repository[Product]) ([]esodm.Hit[Product], error) {
	price := esodm.OrderedValue[int]("price")
	return products.Find(esodm.Match("name", "Go")).
		Filter(esodm.Term("category", "books"), price.LTE(30)).
		Sort(price.Asc()).Size(20).All(ctx)
}
