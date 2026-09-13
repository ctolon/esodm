// Package es8 connects esodm to the official Elasticsearch 8 client.
package es8

import (
	"context"
	"fmt"

	"github.com/ctolon/esodm"
	"github.com/elastic/go-elasticsearch/v8"
)

// Connect accepts a configured official client. Set DisableRetry: true in its
// configuration for writes that cannot safely be replayed after an ambiguous error.
func Connect(ctx context.Context, client *elasticsearch.Client, config esodm.Config) (*esodm.Client, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: nil client", esodm.ErrValidation)
	}
	c, err := esodm.Connect(ctx, client, config)
	if err == nil && c.Version().Major != 8 {
		return nil, fmt.Errorf("%w: es8 requires server major 8", esodm.ErrUnsupported)
	}
	return c, err
}
