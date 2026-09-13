.PHONY: test verify fuzz integration matrix generate benchmark
test:
	go test -race ./...
verify:
	PATH="$(CURDIR)/bin:$$PATH" python3 scripts/verify.py --staticcheck bin/staticcheck --security
fuzz:
	python3 scripts/fuzz.py --duration 30s
integration:
	go test -tags integration -timeout 5m ./integration -args -es-url http://127.0.0.1:19280 -es-major 8 -es-licensed
	go test -tags integration -timeout 5m ./integration -args -es-url http://127.0.0.1:19290 -es-major 9 -es-licensed
matrix: integration
	go test -tags integration -timeout 5m ./integration -args -es-url http://127.0.0.1:19281 -es-major 8 -es-licensed
	go test -tags integration -timeout 5m ./integration -args -es-url http://127.0.0.1:19291 -es-major 9 -es-licensed
generate:
	GOTOOLCHAIN=go1.27.0 go generate ./examples/...
benchmark:
	go test -run '^$$' -bench 'Benchmark(Query|Codec|GetNoJoin|Bulk)$$' -benchmem -count=5 .

.PHONY: fmt
fmt:
	GOTOOLCHAIN=go1.27.0 go fmt ./...

.PHONY: tools lint docs
tools:
	GOBIN="$(CURDIR)/bin" GOTOOLCHAIN=go1.27.0 go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
	GOBIN="$(CURDIR)/bin" GOTOOLCHAIN=go1.27.0 go install honnef.co/go/tools/cmd/staticcheck@v0.8.1
	GOBIN="$(CURDIR)/bin" GOTOOLCHAIN=go1.27.0 go install golang.org/x/vuln/cmd/govulncheck@v1.6.0
	GOBIN="$(CURDIR)/bin" GOTOOLCHAIN=go1.27.0 go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
lint:
	GOTOOLCHAIN=go1.27.0 bin/golangci-lint run ./...
	bin/actionlint
docs:
	GOTOOLCHAIN=go1.27.0 go run ./internal/docgen
	python3 scripts/docs.py --write
