# Getting started

## Requirements

- Go 1.26 or 1.27. Go 1.27 additionally provides the `Client.Repository[T]` and `Client.Load[T]` method forms.
- Elasticsearch 8.18.1 or later within major 8, or 9.4.5 or later within major 9. The tested matrix is listed in [Compatibility](compatibility.md).

```sh
go get github.com/ctolon/esodm
```

## Run the example

```sh
docker compose up -d --wait es8
go run ./examples/basic
```

[examples/basic](../examples/basic/main.go) connects to `http://127.0.0.1:19280`, creates the `esodm-example-products` index, writes one document and searches for it. `docker compose down` removes the cluster.

## Connect

Create an official client and pass it to the adapter for its major. Operation snippets below belong inside a function returning an error; model type/method declarations belong at package level. The runnable example above contains the complete imports and startup function:

```go
raw, err := elasticsearch.NewClient(elasticsearch.Config{
	Addresses:    []string{"https://es.example.internal:9200"},
	APIKey:       apiKey,
	CACert:       caCert,
	DisableRetry: true,
})
if err != nil {
	return err
}
client, err := es8.Connect(ctx, raw, esodm.Config{})
if err != nil {
	return err
}
```

`adapter/es9.Connect` accepts a v9 client. `Connect` requests `GET /`, checks the server version against the supported baseline and rejects a client whose major differs from the server. Addresses, TLS, authentication, compression and transport retries are configured on the official client; the ODM adds no connection pool or whole-request retry layer of its own.

Disable transport retries for writes that must not be replayed: `DisableRetry: true` on v8, or `elasticsearch.WithTransportOptions(elastictransport.WithDisableRetry())` on v9. Bulk item retries are a separate, opt-in policy described in [Bulk processing](bulk.md).

Reuse the connected client and repositories for the lifetime of the application. Pass a context with a deadline to every operation.

## Define a model and a repository

```go
type Product struct {
	esodm.DocumentMeta
	Name     string `json:"name" es:"type=text"`
	Category string `json:"category"`
	Price    int    `json:"price"`
}

func (Product) IndexName() string { return "products" }

schema, err := esodm.NewSchemaFor[Product]()
if err != nil {
	return err
}
products, err := esodm.NewRepository(client, schema)
if err != nil {
	return err
}
if err := products.EnsureIndex(ctx); err != nil {
	return err
}
```

`NewSchemaFor` infers the mapping from the struct and reads the index name from `IndexName`. `NewSchema[Product]("products")` takes the name explicitly. Constructors do not contact the cluster; `EnsureIndex` creates the index if it is missing and leaves an existing index unchanged. [Models and mappings](models.md) covers tags, metadata and hooks.

## Write and read

<!-- source: examples/guide/crud.go -->
```go
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
```

`Create` fails with `ErrConflict` if the ID exists. `Save` replaces the document using the ID and routing carried by `DocumentMeta`. `Update` sends a partial document; `IfMatches` turns the sequence number and primary term of an earlier read into a conditional write. Every write returns a `WriteResult` with the document metadata.

## Search

<!-- source: examples/guide/finder.go -->
```go
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
```

`Find` starts an immutable query chain bound to the repository. `All` returns hits with metadata, `Sources` returns documents, `First` returns one hit, `Page` returns a numbered window and `Each` iterates every match through a point in time. [Finder](finder.md) lists the terminal operations; [Queries and search](search.md) covers the query helpers and `Search` options.

## Next steps

- [Document operations](documents.md) for the complete write contract.
- [Production operations](production.md) for error handling, telemetry and privileges.
- [Bulk processing](bulk.md) for high-volume ingestion.
