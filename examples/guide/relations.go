package guide

import (
	"context"

	"github.com/ctolon/esodm"
)

// RelatedProducts follows same-type references with explicit graph limits.
func RelatedProducts(ctx context.Context, client *esodm.Client, roots []esodm.Ref[Product]) (esodm.Graph[Product], error) {
	return esodm.Preload(ctx, client, roots, func(p Product) []esodm.Ref[Product] {
		return p.Related
	}, esodm.PreloadOptions{MaxDepth: 3, MaxDocuments: 1000})
}
