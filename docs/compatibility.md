# Compatibility

## Supported versions

| Component | Versions |
| --- | --- |
| Go | 1.26 and 1.27 |
| Official Go clients | `go-elasticsearch/v8` 8.19.7 and `go-elasticsearch/v9` 9.5.2 |
| Elasticsearch | 8.18.1 and later within major 8; 9.4.5 and later within major 9 |
| Tested servers | 8.18.1, 8.19.7, 9.4.5, 9.5.2 |
| Platforms | Linux (full test suite); Windows and macOS (build and unit tests) |

`Connect` rejects servers below the minimum version and clients whose major differs from the server. OpenSearch and Elasticsearch Serverless are not supported.

Query, aggregation and mapping helpers use the v9 `typedapi` types as a common model on both majors. Only use options that the connected server supports. Builders passed to `DoTyped` must come from the client library of the connected major.

Go 1.27 adds the `Client.Repository[T]` and `Client.Load[T]` generic methods; the package-level functions provide the same behavior on Go 1.26. Formatting and code generation for this repository require Go 1.27.

## Design boundaries

| Area | Behavior |
| --- | --- |
| Transport | The official client owns connections, authentication, TLS, compression and transport retries. esodm sends requests through its `Perform` method and adds no whole-request retry layer. Opt-in bulk retries replay only explicit transient item failures, reusing prepared payloads. |
| Request models | Official `esdsl` builders and `typedapi` types are the query and mapping language. esodm adds typed field descriptors, immutable snapshots, model mapping and lifecycle hooks. |
| Document paths | Document IDs are path-escaped by esodm so that IDs containing `/`, `.` or `..` are addressed correctly. |
| Model registration | Repositories are registered explicitly. There is no package scanning. |
| Relationships | References are loaded explicitly. There is no identity map, cascade or lazy loading. |
| Server-side operations | Scripts and by-query operations run on the server without Go hooks or timestamp updates. |
| Bulk flushing | Time- and count-based flushing is built in; byte-based flushing uses the official `esutil.BulkIndexer` through the adapters. |
| Migrations | The application pauses writers and holds the migration lock. Ambiguous checkpoint phases require manual reconciliation. |
| Windows checkpoints | `migrationstore.File` syncs file contents but not the directory entry, and `os.Rename` is not atomic on Windows. |
| PIT parameters | Routing and preference are applied when the PIT is opened and cannot be set on PIT page requests. |
| Callback panics | Panics in hooks, observers and bulk callbacks are not recovered by the library. |
| Administration | Index and alias administration stays in the root package because it shares the client, schema and error types. |

## API stability

Before 1.0, a minor release may include breaking changes, which are listed under a "Breaking" heading in the changelog; patch releases are backward compatible. From 1.0, the exported API follows Go module compatibility rules. A deprecated API remains available for at least one minor release before removal. `internal/` packages, the generator's implementation and command-line tools are not part of the compatibility contract.
