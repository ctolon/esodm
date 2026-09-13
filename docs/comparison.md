# Comparison with Python DSL and Spring Data Elasticsearch

This is a conceptual guide, not a performance ranking or an API compatibility promise. Consult the [Python DSL reference](https://www.elastic.co/docs/reference/elasticsearch/clients/python/elasticsearch-dsl) and [Spring Data Elasticsearch reference](https://docs.spring.io/spring-data/elasticsearch/reference/) for their version-specific behavior.

## Concepts

| Concern | esodm | Python DSL | Spring Data Elasticsearch |
| --- | --- | --- | --- |
| Model | Go struct with `json` and `es` tags | `Document` subclass with field declarations | Annotated entity |
| Index name | `IndexName()` method | Inner `Index` class | `@Document(indexName = ...)` |
| Document ID | Explicit argument, or `Identified` on the model | `meta.id` | `@Id` |
| Shared fields | Embedding | Inheritance | Inheritance |
| Index creation | `EnsureIndex`, `Registry` | `Document.init()` | `createIndex` on `@Document` |
| Queries | `esdsl` builders and typed descriptors, immutable values | `Search` and `Q` composition | `NativeQuery` and repository methods |
| Chained reads | `Finder` with terminal operations | `Search` execution | Repository and `ElasticsearchOperations` |
| Aggregations | `Aggregations`, `DecodeAgg`, `Aggregates` | `Search.aggs`, response aggregations | `withAggregation`, `ElasticsearchAggregations` |
| Multi-get | `MGet`, `Load` | `Document.mget` | `findAllById` |
| Bulk | `Bulk`, `BulkSeq`, `BulkStream`, esutil adapter | `helpers.bulk` | `saveAll`, bulk operations |
| Full scan | `Iterate`, `Each`, cursors | PIT and `search_after` | PIT with `search_after` |
| Audit timestamps | `Timestamps` | Application code | Auditing annotations |
| Escape hatch | `DoTyped`, `SearchFromRequest`, `Do` | Official client | Official client |

esodm keeps loading, index initialization and migrations explicit. It does not provide lazy loading, cascades or an identity map. The official Go client remains responsible for transport configuration. See [design boundaries](compatibility.md#design-boundaries) and the [Go examples](../examples/guide).
