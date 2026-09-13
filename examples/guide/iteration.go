package guide

import (
	"context"
	"errors"

	"github.com/ctolon/esodm"
)

// VisitProducts owns its PIT and preserves cleanup errors after early returns.
func VisitProducts(ctx context.Context, repo *esodm.Repository[Product], visit func(Product) error) (err error) {
	it, err := repo.Iterate(ctx, esodm.NewSearch(esodm.MatchAll()), 100, "1m")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, it.Close()) }()
	for it.Next() {
		if err := visit(it.Hit().Source); err != nil {
			return err
		}
	}
	return it.Err()
}
