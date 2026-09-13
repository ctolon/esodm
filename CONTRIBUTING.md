# Contributing

Contributions are accepted under the Apache-2.0 license. Please read the [code of conduct](CODE_OF_CONDUCT.md) and report security issues through the [security policy](SECURITY.md) rather than a public issue.

## Toolchain

The library supports Go 1.26 and 1.27. Develop with Go 1.27: `generic_go127.go` uses generic methods behind a build constraint, and `gofmt`, `go generate` and the documentation generator need a 1.27 toolchain to parse it. Keep the package API usable on Go 1.26.

## Local checks

```sh
make tools      # installs pinned golangci-lint, staticcheck, govulncheck and actionlint into ./bin
make fmt
make generate   # regenerates example descriptors
make docs       # regenerates docs/api.md and embedded examples
make lint
make verify     # race tests, vet, staticcheck, govulncheck and the 90% core coverage gate
make fuzz
make benchmark  # small local Go benchmarks; no Elasticsearch required
```

Run the live integration matrix before changing request encoding, lifecycle behavior or migration logic:

```sh
docker compose --profile matrix up -d --wait
python3 scripts/matrix.py --race
docker compose --profile matrix down
```

`scripts/secure.py --major 8` (or `9`) runs the TLS and restricted-API-key tests against a disposable secured node. The Compose services bind to loopback with security disabled; do not point them at production data.

## Guidelines

- Prefer official client request models and builders over new Elasticsearch structures in this package.
- Keep ownership, cancellation, partial outcomes and retry behavior explicit in code and documentation.
- Add regression tests for behavior changes. Use fuzz tests for parsers and input boundaries where generated inputs provide useful coverage.
- Every exported declaration and named public struct field needs a doc comment that describes behavior and constraints. Do not reference Markdown files from Go comments.
- Put complete examples in `examples/guide`; the guides embed them and `make docs` keeps the copies in sync.
- Record user-visible changes in `CHANGELOG.md`.

Open focused pull requests using the template and list the checks you ran. Performance claims must come from repeatable [Go benchmarks](docs/benchmarks.md) on the same host.

Keep documentation and code comments in English. Describe behavior, defaults and limits directly; omit development-session reports and unverified performance claims.

Discuss substantial API changes in an issue before implementing them. You do not need to sign a CLA. By submitting a contribution, you agree to license it under Apache-2.0.
