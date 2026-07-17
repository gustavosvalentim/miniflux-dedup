GOLANGCI_LINT_VERSION := 2.7.2

.PHONY: build check clean lint test typecheck

build:
	go build -trimpath -o bin/miniflux-dedup ./cmd/miniflux-dedup

test:
	go test -race ./...

typecheck:
	go vet ./...

lint:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)
	@golangci-lint --version | grep -q "version $(GOLANGCI_LINT_VERSION) " || (echo "golangci-lint $(GOLANGCI_LINT_VERSION) is required" && exit 1)
	golangci-lint run ./...

check: lint typecheck test build

clean:
	rm -rf bin
