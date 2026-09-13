package guide

import (
	"context"

	"github.com/ctolon/esodm"
)

// CreateProduct creates a document and waits for search visibility.
func CreateProduct(ctx context.Context, repo *esodm.Repository[Product]) (esodm.WriteResult, error) {
	return repo.Create(ctx, "book-1", Product{Name: "Go book", Category: "books", Price: 25},
		esodm.WithRefresh(esodm.RefreshWaitFor))
}

// ChangePrice uses both concurrency tokens from the last read.
func ChangePrice(ctx context.Context, repo *esodm.Repository[Product], id, routing string, price int) (esodm.WriteResult, error) {
	hit, err := repo.GetWith(ctx, id, esodm.ReadOptions{Routing: routing})
	if err != nil {
		return esodm.WriteResult{}, err
	}
	patch, err := esodm.NewPatch(esodm.OrderedValue[int]("price").Set(price))
	if err != nil {
		return esodm.WriteResult{}, err
	}
	return repo.Update(ctx, id, patch, esodm.IfMatches(hit.Metadata))
}

// SaveProduct replaces a document using model ID and routing.
func SaveProduct(ctx context.Context, repo *esodm.Repository[Product]) (esodm.WriteResult, error) {
	return repo.Save(ctx, Product{
		DocumentMeta: esodm.DocumentMeta{ID: "book-2", Routing: "tenant-1"},
		Name:         "Search systems", Price: 40,
	})
}
