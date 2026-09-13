# Benchmarks

The package keeps four local Go benchmarks for query construction, JSON encoding/decoding, repository GET response handling and a 100-document bulk operation. They use an in-memory transport and require no Elasticsearch, Python or JVM service.

```sh
make benchmark
# Equivalent command:
go test -run '^$' -bench 'Benchmark(Query|Codec|GetNoJoin|Bulk)$' -benchmem -count=5 .
```

## Reading results

`ns/op` measures elapsed time per operation, `B/op` allocated bytes and `allocs/op` allocation count. The GET benchmark includes request construction and response decoding, but no network or server execution. The bulk batch case includes lifecycle preparation, encoding and decoding a synthetic successful response; it does not measure cluster indexing throughput. The codec benchmark reports encode and decode separately. The bulk benchmark also isolates preparation of one external indexer item.

Compare before and after on the same machine, Go version, inputs and settings. Record `go version`, CPU model, operating system, `GOMAXPROCS` and the exact command. Retain all repetitions; do not select the fastest sample. Use allocation counts to explain timing changes, and investigate noisy measurements before claiming an improvement. Optional `benchstat` analysis can help compare saved outputs; it is not a required dependency.

Benchmarks do not run as a timing-based CI gate. Unit, race, integration and resource cleanup tests remain the correctness gates. Real cluster throughput depends on mappings, document size, refresh policy, shard layout, cluster capacity and workload; these local measurements cannot predict it.
