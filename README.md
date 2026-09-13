# esodm

esodm is an object-document mapper for Elasticsearch written in Go. It builds on the official `go-elasticsearch` client and adds struct-to-mapping inference, typed repositories, immutable query and search values, bulk processing, point-in-time iteration, relationship loading and reindex migrations.

Module `github.com/ctolon/esodm` · License Apache-2.0 · Go 1.26 and 1.27 · Elasticsearch 8.18.1+ and 9.4.5+

## Installation

```sh
go get github.com/ctolon/esodm
```

Connect through the adapter that matches your server major: `adapter/es8` with `github.com/elastic/go-elasticsearch/v8`, or `adapter/es9` with `github.com/elastic/go-elasticsearch/v9`. Authentication, TLS, compression and transport retries are configured on the official client.

## Example

<!-- source: examples/basic/main.go -->
```go
// A runnable example. Start the compose ES 8 service before running it.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ctolon/esodm"
	"github.com/ctolon/esodm/adapter/es8"
	"github.com/elastic/go-elasticsearch/v8"
)

//go:generate go run ../../cmd/esodmgen -type Product
type Review struct {
	Text  string `json:"text" es:"type=text"`
	Stars int    `json:"stars"`
}
type Product struct {
	Name    string               `json:"name" es:"type=text"`
	Price   int                  `json:"price"`
	Reviews []Review             `json:"reviews" es:"type=nested"`
	Related []esodm.Ref[Product] `json:"related"`
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raw, err := elasticsearch.NewClient(elasticsearch.Config{Addresses: []string{"http://127.0.0.1:19280"}, DisableRetry: true})
	if err != nil {
		return err
	}
	c, err := es8.Connect(ctx, raw, esodm.Config{})
	if err != nil {
		return err
	}
	schema, err := esodm.NewSchema[Product]("esodm-example-products")
	if err != nil {
		return err
	}
	products, err := esodm.NewRepository(c, schema)
	if err != nil {
		return err
	}
	if err = products.EnsureIndex(ctx); err != nil {
		return err
	}
	_, err = products.Replace(ctx, "book", Product{Name: "Go search book", Price: 25, Reviews: []Review{{Text: "Useful", Stars: 5}}, Related: []esodm.Ref[Product]{{Index: schema.Index(), ID: "book"}}}, esodm.WriteOptions{Refresh: "wait_for"})
	if err != nil {
		return err
	}
	result, err := products.Search(ctx, esodm.NewSearch(esodm.And(ProductFields.Name.Match("Go"), ProductFields.Price.LTE(30))))
	if err != nil {
		return err
	}
	for _, hit := range result.Hits.Hits {
		related, err := ProductFields.Related.Load(ctx, c, hit.Source.Related...)
		if err != nil {
			return err
		}
		fmt.Printf("%s: %s (%d references)\n", hit.ID, hit.Source.Name, len(related))
	}
	return nil
}
func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
```

A runnable version is in [examples/basic](examples/basic/main.go):

```sh
docker compose up -d --wait es8
go run ./examples/basic
```

## Documentation

- [Documentation guide](docs/README.md): models, document operations, search, bulk processing, migrations and production operations.
- [API reference](docs/api.md): generated from the exported declarations.
- [Compatibility](docs/compatibility.md): supported versions, design boundaries and API stability policy.
- [Comparison with Python DSL and Spring Data Elasticsearch](docs/comparison.md).
- [Benchmarks](docs/benchmarks.md).

## Development

```sh
make tools     # pinned linters and scanners in ./bin
make lint      # golangci-lint and actionlint
make verify    # race tests, vet, staticcheck, govulncheck, coverage gate
make docs      # regenerate the API reference and embedded examples
make fuzz      # 30-second fuzz smoke run
```

See [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md) and [RELEASING.md](RELEASING.md).

## License

[Apache-2.0](LICENSE). Release notes are in [CHANGELOG.md](CHANGELOG.md).
