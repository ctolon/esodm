//go:build go1.27

package esodm

import "context"

// Repository is the Go 1.27 method form of NewRepository.
func (c *Client) Repository[T any](schema *Schema[T], options ...RepositoryOption[T]) (*Repository[T], error) {
	return NewRepository(c, schema, options...)
}

// Load is the Go 1.27 method form of Load.
func (c *Client) Load[T any](ctx context.Context, refs []Ref[T]) ([]ReferenceResult[T], error) {
	return Load(ctx, c, refs)
}
