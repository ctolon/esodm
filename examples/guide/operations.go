package guide

import (
	"context"
	"encoding/json"

	"github.com/ctolon/esodm"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/deletebyquery"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/update"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/updatebyquery"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

// IncrementPrice runs a server-side script with a bounded conflict retry count.
func IncrementPrice(ctx context.Context, repo *esodm.Repository[Product], id string) (esodm.UpdateResult[Product], error) {
	return repo.UpdateWith(ctx, id, &update.Request{
		Script:  &types.Script{Source: "ctx._source.price += params.step", Params: map[string]json.RawMessage{"step": json.RawMessage("1")}},
		Source_: true,
	}, esodm.UpdateOptions{RetryOnConflict: 3})
}

// RepriceBooks submits an asynchronous operation and returns its task response.
func RepriceBooks(ctx context.Context, repo *esodm.Repository[Product]) (updatebyquery.Response, error) {
	return repo.UpdateByQuery(ctx, &updatebyquery.Request{
		Query:  &types.Query{Term: map[string]types.TermQuery{"category": {Value: "books"}}},
		Script: &types.Script{Source: "ctx._source.price = params.price", Params: map[string]json.RawMessage{"price": json.RawMessage("30")}},
	}, esodm.ByQueryOptions{Async: true})
}

// DeleteCategory explicitly scopes deletion by category.
func DeleteCategory(ctx context.Context, repo *esodm.Repository[Product], category string) (deletebyquery.Response, error) {
	return repo.DeleteByQuery(ctx, &deletebyquery.Request{
		Query: &types.Query{Term: map[string]types.TermQuery{"category": {Value: category}}},
	}, esodm.ByQueryOptions{Refresh: true})
}
