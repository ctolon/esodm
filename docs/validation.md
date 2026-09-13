# Testing and CI

## Local checks

Use Go 1.27 for development tools and generation. The package is also tested with Go 1.26.

```sh
make tools
make fmt
make generate
make docs
make lint
GOTOOLCHAIN=go1.27.0 make verify
GOTOOLCHAIN=go1.26.0 go test -race -count=1 ./...
python3 -m unittest discover -s scripts -p 'test_*.py'
make fuzz
make benchmark
```

`make verify` runs race tests, vet, staticcheck, govulncheck and coverage checks. Minimum coverage is 90% for the core package, migration and geo code, and 80% for each client adapter, the checkpoint store and the recorder. Generated descriptors, API documentation and embedded examples must match their sources.

## Elasticsearch tests

The integration matrix covers both Go versions against Elasticsearch 8.18.1, 8.19.7, 9.4.5 and 9.5.2. Tests include mappings, binary containers, document operations, search, bulk writes, pagination, relationships and migrations.

```sh
docker compose --profile matrix up -d --wait
python3 scripts/matrix.py --race
docker compose --profile matrix down
GOTOOLCHAIN=go1.27.0 python3 scripts/secure.py --major 8
GOTOOLCHAIN=go1.27.0 python3 scripts/secure.py --major 9
```

These commands create disposable local clusters. The ordinary matrix disables security and uses trial licenses. The secured tests use TLS and restricted API keys, including a request that must return 403 outside the permitted index pattern. Use test data only. The secure helper removes its containers on exit. Disposable TLS fixture files remain under ignored `artifacts/secure/certs/`; delete that directory when finished. Passwords and API keys are passed through the process environment.

## Workflow checks

| Workflow | Trigger | Checks |
| --- | --- | --- |
| `ci` | Pull requests, merge queue, pushes to `main`, weekly schedule and manual runs | Linux unit/race tests on Go 1.26/1.27; native Windows/macOS tests; lint, vulnerability scan and coverage; generated files and documentation; eight integration combinations; TLS tests for both majors; ten fuzz targets |
| `release verification` | Manual runs and release tags through the release workflow | 60 minutes per fuzz target and two hours of endurance testing per Elasticsearch major |
| `release draft` | `v*` tags or manual runs on a tag | Version, changelog and `main` ancestry validation; both workflows above; source archive, checksum and draft release |

Normal CI fuzz runs use 30 seconds per target; scheduled runs use 10 minutes. The stable `CI checks` job fails if any required job fails, is cancelled or is skipped. Use that job as the required branch check. Artifacts contain test output and coverage reports; never upload secured cluster configuration or keys.

Release verification runs on the tagged revision. A draft is created only after all release checks pass. See [Releasing](../RELEASING.md) for repository settings, first-commit commands and publication steps. Workflow configuration is not proof of a successful run: inspect the run associated with the candidate commit before publishing.
