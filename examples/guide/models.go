// Package guide contains compile-checked application examples.
package guide

import (
	"context"
	"fmt"

	"github.com/ctolon/esodm"
)

// Product stores its metadata outside the JSON source.
type Product struct {
	esodm.DocumentMeta
	esodm.Timestamps
	Name     string               `json:"name" es:"type=text"`
	Category string               `json:"category"`
	Price    int                  `json:"price"`
	Reviews  []Review             `json:"reviews" es:"type=nested"`
	Location esodm.GeoPoint       `json:"location"`
	Related  []esodm.Ref[Product] `json:"related"`
}

// Review preserves the relationship between text and rating within one review.
type Review struct {
	Text   string `json:"text" es:"type=text"`
	Rating int    `json:"rating"`
}

// IndexName supplies the default index for Product.
func (Product) IndexName() string { return "products" }

// Products constructs a repository without changing the cluster.
func Products(client *esodm.Client) (*esodm.Repository[Product], error) {
	schema, err := esodm.NewSchemaFor[Product]()
	if err != nil {
		return nil, err
	}
	return esodm.NewRepository(client, schema, esodm.Hooks[Product]{
		Validate: func(_ context.Context, p *Product) error {
			if p.Price < 0 {
				return fmt.Errorf("%w: negative price", esodm.ErrValidation)
			}
			return nil
		},
	})
}

// Initialize creates missing indexes in registration order.
func Initialize(ctx context.Context, repositories ...esodm.Ensurer) error {
	var registry esodm.Registry
	if err := registry.Add(repositories...); err != nil {
		return err
	}
	return registry.EnsureAll(ctx)
}
