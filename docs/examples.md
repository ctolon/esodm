# Example catalog and behavioral coverage

The examples use the same public API as an application. Start with the [runnable application](../examples/basic/main.go), then use the topic guides for complete functions. The [option contracts](options.md) explain accepted values and defaults, including settings omitted by a particular example.

## Run examples

The deterministic examples use `esodmtest.Recorder` and need no server:

```sh
go test -run '^Example' -v .
go test ./examples/...
```

Each `Example` with an `Output` comment is executed by `go test`; its output is checked. The first command exercises query serialization, reads, writes, optional values, conflicts, committed errors, multi-get, bulk retry and early iterator cleanup. The second compiles the application guide packages. Compilation alone is not evidence that Elasticsearch accepted a request.

Run the real application against a disposable ES 8 node:

```sh
docker compose up -d --wait es8
go run ./examples/basic
```

Expected output is `book: Go search book (1 references)`. The example uses a fixed index and replaces the same document on repeat runs. It leaves that example index available for inspection.

For cluster behavior, start the four services and run the existing integration matrix:

```sh
docker compose --profile matrix up -d --wait
python3 scripts/matrix.py --race
```

The matrix exercises both supported Go versions. TLS/restricted-key and endurance tests have separate prerequisites described in [Validation](validation.md). Advanced capabilities can require a trial license, appropriate mappings or a provisioned inference endpoint; see [Advanced search](advanced-search.md).

## Find a scenario

| Behavior | Application examples | Regression or integration coverage |
| --- | --- | --- |
| Connect, schema, CRUD, basic search | [Runnable application](../examples/basic/main.go), [getting started](getting-started.md) | [connection and document tests](../esodm_test.go), [live documents and escaped IDs](../integration/integration_test.go) |
| Models, metadata, timestamps, initialization | [Models](../examples/guide/models.go), [mapping guide](models.md) | [model and feature contracts](../features_test.go), [field boundaries](../readiness_test.go) |
| Binary fields, optional blobs and arrays of blobs | [Mapping contracts](models.md), [complete write/read scenario](../integration/binary_test.go) | [binary inference regression](../production_contract_test.go), [live binary round trip](../integration/binary_test.go) |
| Normalize and validate through hooks | [Hooks](../examples/guide/hooks.go), [committed error example](../example_contracts_test.go) | [application hook tests](../examples/testing/recorder_test.go), [caller ownership and nil contracts](../production_contract_test.go), [lifecycle tests](../boundaries_test.go) |
| Nil versus explicit false, option precedence | [Executable option examples](../example_contracts_test.go), [all options](options.md) | [read/write options](../features_test.go), [production contracts](../production_contract_test.go) |
| Optimistic concurrency and patch updates | [CRUD functions](../examples/guide/crud.go), [conflict example](../example_contracts_test.go) | [live guide workflow](../integration/guide_test.go), [document tests](../esodm_test.go) |
| Immutable query composition, filtering, projection | [Search](../examples/guide/search.go), [query examples](../example_test.go) | [native snapshots](../native_test.go), [search boundaries](../review_test.go) |
| Finder, numbered pages and typed fields | [Finder](../examples/guide/finder.go), [generated model descriptors](../examples/models/esodm_fields.gen.go) | [Finder tests](../finder_test.go), [live Finder](../integration/finder_test.go), [generator tests](../cmd/esodmgen/main_test.go) |
| Aggregations, typed keys and disjunctive facets | [Facets](../examples/guide/facets.go), [typed aggregate example](../example_test.go) | [facet contracts](../features_test.go), [live facets](../integration/features_test.go) |
| Nested, geo, percolation, vectors and hybrid search | [Advanced requests](../examples/guide/advanced.go), [advanced guide](advanced-search.md) | [native request tests](../native_test.go), [live mapping and requests](../integration/native_test.go), [licensed hybrid](../integration/integration_test.go) |
| PIT iteration, early stop and cleanup errors | [Explicit iteration](../examples/guide/iteration.go), [early-stop example](../example_test.go) | [cursor boundaries](../boundaries_test.go), [live cursor resumption](../integration/integration_test.go), [leak checks](../leak_test.go) |
| Missing and duplicate references, preloading, joins | [Relations](../examples/guide/relations.go), [multi-get example](../example_contracts_test.go) | [relation features](../features_test.go), [live parent-child](../integration/integration_test.go) |
| Bulk sequence import and producer errors | [Bulk import](../examples/guide/bulk.go) | [stream contracts](../features_test.go), [live guide import](../integration/guide_test.go) |
| Partial bulk failures, retry and unknown outcomes | [Retry example](../example_contracts_test.go), [bulk guide](bulk.md) | [bulk boundaries](../boundaries_test.go), [malformed response regression](../production_contract_test.go) |
| External indexer and completion callbacks | [esutil indexer](../examples/guide/indexer.go) | [adapter contracts](../adapter_contract_test.go), [ES 8 adapter](../adapter/es8/bulk_test.go), [ES 9 adapter](../adapter/es9/bulk_test.go), [live indexer](../integration/features_test.go) |
| Scripts, by-query operations, tasks and cancellation | [Operations](../examples/guide/operations.go) | [operation contracts](../features_test.go), [live guide workflow](../integration/guide_test.go) |
| Index resources, alias migration and checkpoint recovery | [Migration](../examples/guide/migration.go), [administration](migrations.md) | [administration](../admin_test.go), [checkpoint phases](../migration_features_test.go), [checkpoint storage](../migrationstore/file_test.go), [live migration](../integration/features_test.go) |
| Telemetry, redaction and least-privilege access | [Metrics observer](../examples/guide/telemetry.go), [production guide](production.md) | [observer tests](../observe/otel/observer_test.go), [telemetry boundaries](../readiness_test.go), [TLS and permissions](../integration/secure_test.go) |
| Malformed JSON, cursors, tags and size limits | [Errors and limits](options.md), [error examples](../example_contracts_test.go) | [fuzz targets](../fuzz_test.go), [request/response boundaries](../boundaries_test.go) |

## Adding a case

Add the application function or executable `Example` first, then a regression scenario that checks externally observable behavior. Put source-backed examples in the relevant guide using its `source` marker, run `make docs`, and update this catalog when adding a new behavior family. CI compiles the examples, runs executable output assertions, checks public field comments and verifies embedded copies and local link anchors.
