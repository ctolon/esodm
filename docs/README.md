# esodm documentation

| Guide | Contents |
| --- | --- |
| [Getting started](getting-started.md) | Installation, connections, first write and first search |
| [Models and mappings](models.md) | Struct mapping, tags, metadata, timestamps, hooks, initialization and code generation |
| [Document operations](documents.md) | Create, insert, save, replace, update, upsert, delete and optimistic concurrency |
| [Finder](finder.md) | Query chains and terminal operations |
| [Queries and search](search.md) | Query helpers, search options and results |
| [Aggregations and facets](aggregations.md) | Metric and bucket aggregations, typed decoding and disjunctive facets |
| [Advanced search](advanced-search.md) | Nested, geo, percolate, semantic, vector and hybrid retrieval |
| [Pagination](pagination.md) | Point-in-time iteration, cursors and resumption |
| [Relationships](relationships.md) | References, preloading and parent-child joins |
| [Bulk processing](bulk.md) | Batches, retries, streams and the esutil adapter |
| [Scripts and tasks](operations.md) | Script updates, update and delete by query, task management |
| [Administration and migrations](migrations.md) | Index resources, aliases and checkpointed reindex migrations |
| [Production operations](production.md) | Connections, errors, telemetry, privileges, memory and testing |
| [Compatibility](compatibility.md) | Supported versions, design boundaries and API stability |
| [Testing and CI](validation.md) | Local checks, integration tests and release gates |
| [Comparison](comparison.md) | Conceptual comparison with Python DSL and Spring Data Elasticsearch |
| [Benchmarks](benchmarks.md) | Small Go benchmarks, reproduction and interpretation |
| [Example catalog](examples.md) | Runnable examples, prerequisites, expected output and behavioral test mapping |
| [Options and contracts](options.md) | Defaults, optional values, limits, ownership and failure semantics |
| [API reference](api.md) | Every exported declaration |

Go examples in the guides are copied from [examples/guide](../examples/guide), which is compiled in CI. The API reference is generated from Go doc comments.

Contributor documentation: [Contributing](../CONTRIBUTING.md), [Security](../SECURITY.md), [Releasing](../RELEASING.md).
