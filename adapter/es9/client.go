// Package es9 connects esodm to the official Elasticsearch 9 client.
package es9

import (
	"context"
	"fmt"

	"github.com/ctolon/esodm"
	"github.com/elastic/go-elasticsearch/v9"
)

// Connect binds a configured official client and verifies server major 9.
// Configure authentication, retries and pooling on the official client.
func Connect(ctx context.Context, client *elasticsearch.Client, config esodm.Config) (*esodm.Client, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: nil client", esodm.ErrValidation)
	}
	c, err := esodm.Connect(ctx, client, config)
	if err == nil && c.Version().Major != 9 {
		return nil, fmt.Errorf("%w: es9 requires server major 9", esodm.ErrUnsupported)
	}
	return c, err
}
