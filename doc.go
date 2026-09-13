// Package esodm maps Go documents to Elasticsearch using the official client.
// All network operations require a context. Repositories are safe for concurrent
// use after construction; documents remain owned by the caller. Writes are not
// transactions across documents and search visibility follows Elasticsearch's
// refresh policy.
package esodm
