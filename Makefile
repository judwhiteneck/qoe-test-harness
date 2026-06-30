GO ?= go

.PHONY: all test race vet fmt fmtcheck lint check-imports ci build

all: ci

build:
	$(GO) build ./...

test:
	$(GO) test -cover ./...

race:
	CGO_ENABLED=1 $(GO) test -race -cover ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

fmtcheck:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

lint:
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed; skipping"

check-imports:
	@bash scripts/check-imports.sh

# What CI runs. Local `make ci` mirrors it (race needs a C compiler for cgo).
ci: fmtcheck vet check-imports test
