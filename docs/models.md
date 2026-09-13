# Models and mappings

## Define a model

A repository's type parameter is a struct. Exported fields with JSON tags become mapped properties. Embedded structs are flattened the way `encoding/json` flattens them, so shared fields and behavior can live in reusable types. `DocumentMeta` carries the ID and routing outside `_source`; `Timestamps` adds `created_at` and `updated_at` to the source. Neither is required.

<!-- source: examples/guide/models.go -->
```go
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
```

`IndexName` must return the index or alias for a zero-value model; value and pointer receivers both work. Use `NewSchema[T](index)` when the name is not fixed by the type, for example per-tenant indexes or migration targets.

## Mapping inference

| Go type | Mapping |
| --- | --- |
| `string` | `keyword`; use `es:"type=text"` for analyzed text |
| `bool` | `boolean` |
| signed integers | `long` |
| unsigned integers | `unsigned_long` |
| floats | `double` |
| `time.Time` | `date` |
| `[]byte`, `*[]byte`, arrays/slices of byte slices | `binary`; explicit mapping options are retained |
| `[N]byte` | `unsigned_long` array, matching JSON numeric-array encoding |
| `esodm.GeoPoint` | `geo_point` |
| struct or slice of structs | `object`; use `es:"type=nested"` for independently queried array elements |
| map or interface | no inference; an explicit `type` tag is required |

Pointers, slices and arrays map to their element type. Field names come from the `json` tag; fields tagged `json:"-"` are excluded, and `es:"-"` requires that exclusion. Two embedded fields promoting the same name at the same depth is an error unless exactly one carries a JSON tag.

## Mapping tags

The `es` tag is a comma-separated list of `key=value` options:

| Key | Value | Notes |
| --- | --- | --- |
| `type` | mapping type | overrides the inferred type |
| `analyzer`, `search_analyzer` | analyzer name | |
| `format` | date format | |
| `index`, `doc_values` | `true` or `false` | |
| `ignore_above` | integer | |
| `null_value` | JSON scalar | for example `null_value=0` or `null_value="n/a"` |
| `copy_to` | field names separated by `\|` | `copy_to=title_all\|search_all` |
| `dims`, `similarity` | vector dimensions and similarity | |
| `fields` | `keyword` | adds a `keyword` multi-field |
| `dynamic` | `strict`, `true`, `false`, `runtime` | for object and nested fields |
| `inference_id` | inference endpoint | for `semantic_text` fields |

Options outside this list, including arbitrary multi-fields and analyzer definitions, use `SchemaConfig.Properties` or `SchemaFromMapping`.

## Schema configuration

`NewSchema` and `NewSchemaFor` accept options:

```go
schema, err := esodm.NewSchemaFor[Product](
	esodm.WithDynamic(esodm.DynamicFalse),
	esodm.WithSettings(map[string]any{"number_of_shards": 3}),
	esodm.WithProperties(map[string]esodm.FieldMapping{
		"name": {Type: "text", Fields: map[string]esodm.FieldMapping{"keyword": {Type: "keyword"}}},
	}),
)
if err != nil {
	return err
}
```

`WithProperties` overrides inferred fields; an override that contradicts an explicit tag on the same field is rejected. `Dynamic` defaults to `strict`. A `SchemaConfig` value is itself an option and replaces the whole configuration.

`SchemaFromMapping[T](index, mapping, settings)` accepts the official `types.TypeMapping` and `types.IndexSettings` and skips inference. `Schema.Mapping()` and `Schema.Settings()` return the serialized snapshots.

## Metadata and audit fields

- `Identified.DocumentID()` supplies the ID for `Save` and for bulk index and create operations without an explicit ID.
- `Routed.DocumentRouting()` supplies the default write routing; an explicit option wins.
- `MetadataReceiver.SetDocumentMetadata` is called with the hit metadata after `Get`, `MGet`, `Search` and `MultiSearch`, before `AfterRead`.
- `DocumentMeta` implements all three with `json:"-"` fields, so the values never enter `_source`. `Insert` returns the generated ID in `WriteResult` and does not modify the model.

`Timestamps` sets `CreatedAt` on the first write when it is zero and `UpdatedAt` on every full-document write. Partial updates receive an `updated_at` value unless the patch already contains one. `Config.Now` replaces the clock; values are stored in UTC. A custom `Stamper` implements `Stamp` and `StampFields`; the named fields must be top-level `date` or `date_nanos` properties, which `NewSchema` checks.

## Hooks

<!-- source: examples/guide/hooks.go -->
```go
package guide

import (
	"context"
	"fmt"
	"strings"

	"github.com/ctolon/esodm"
)

// ProductHooks demonstrates normalization and validation without external side effects.
// Optional callbacks remain nil. Full-document validation does not run for patches.
func ProductHooks() esodm.Hooks[Product] {
	return esodm.Hooks[Product]{
		BeforeWrite: func(_ context.Context, _ esodm.Operation, product *Product) error {
			product.Name = strings.TrimSpace(product.Name)
			return nil
		},
		Validate: func(_ context.Context, product *Product) error {
			if product.Name == "" || product.Price < 0 {
				return fmt.Errorf("name is required and price must be nonnegative")
			}
			return nil
		},
		BeforePatch: func(_ context.Context, _ string, patch esodm.Patch) error {
			if _, changesName := patch["name"]; changesName {
				name, ok := patch["name"].(string)
				if !ok || strings.TrimSpace(name) == "" {
					return fmt.Errorf("%w: name must be a nonempty string", esodm.ErrValidation)
				}
				patch["name"] = strings.TrimSpace(name)
			}
			return nil
		},
	}
}
```

Full-document writes run stamping, `BeforeWrite`, `Validate`, the request and then `AfterWrite`. Partial updates run stamping and `BeforePatch` on a copy of the patch; they do not validate the full document. Upsert validates the create candidate. Deletes run `BeforeDelete`. `AfterRead` runs after decoding and metadata hydration. An `AfterWrite` failure after a successful request is returned as `CommittedError`. Hooks run concurrently across requests and must be safe for that.

## Initialization and verification

`EnsureIndex` creates a missing index from the schema and does nothing for an existing one. `Registry` groups repositories:

```go
var registry esodm.Registry
if err := registry.Add(products, orders); err != nil {
	return err
}
if err := registry.EnsureAll(ctx); err != nil {
	return err
}
drift, err := registry.VerifyAll(ctx)
if err != nil {
	return err
}
```

`VerifyIndex` compares the live mapping of one index with the schema and returns a `MappingDrift` listing missing, changed and extra top-level fields and changed mapping options. It changes nothing. A difference caused by server-side normalization (for example a defaulted option) is reported and needs review. Use it at startup when a mapping mismatch should stop deployment. Changing an existing mapping is a [migration](migrations.md).

`WithIndex(name)` derives a repository with the same client, hooks and mapping for another index. A concrete name supports every operation. Wildcards, comma-separated lists and `remote:index` targets support `Search`, `MultiSearch`, `Count` and PIT iteration; each hit reports its actual `_index`. Document writes and `EnsureIndex` require a concrete local name.

## Code generation

`esodmgen` generates typed field descriptors for a model:

```go
//go:generate go run github.com/ctolon/esodm/cmd/esodmgen -type Product
```

The generator uses `go/types`, so it resolves embedded and imported types, pointers, aliases and nested structs. Descriptors include `TextField`, `OrderedField`, `DateField`, `GeoField`, `RelationField` and nested `ObjectField` groups; see [examples/models](../examples/models/esodm_fields.gen.go). Run it with Go 1.27. Recursive types, generic roots and embedded types with custom JSON marshalers need hand-written descriptors built with `NewField`, `Object`, `ChildField`, `Text`, `Date` and `Geo`.

See [Options and contracts](options.md) for the full tag grammar, override precedence, nil/zero semantics and ownership rules.

### Generator flags and output

| Flag | Accepted value | Default |
| --- | --- | --- |
| `-type` | Required comma-separated names of concrete structs in the selected package, for example `Product,Review` | Empty is an error |
| `-dir` | Directory containing the model package | `.` (current working directory) |
| `-output` | Go filename without a directory component | `esodm_fields.gen.go` |

The output is written inside `-dir`. An existing file is replaced only if it starts with the esodmgen generated-file marker. Keep hand-written code in a separate file. The generator honors the running toolchain's build constraints, skips test files and the selected output file, and requires one package. It needs dependencies available for type resolution. Errors go to stderr and produce a nonzero exit status; generated files should be checked into the repository and verified for freshness in CI.

Generated descriptors encode Go types and field names; they do not inspect a running cluster or guarantee that a query is appropriate for its mapping. Regenerate after changing model fields, JSON names, mapping tags or embedded types.

Collection descriptors generally describe one element for querying. For a patch replacing a complete slice or array, use `NewField[[]Element]("field").Set(values)` or an explicit Patch. Byte slices are a special scalar representation: their descriptors retain `[]byte` because JSON encodes a blob as base64. Custom JSON marshalers can change the wire representation of a named Go type; use an explicit mapping and matching hand-written descriptors when that representation differs from inferred Go kinds.
