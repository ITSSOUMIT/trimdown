BINARY := trimdown
PKG := ./cmd/trimdown
VERSION ?= dev
LDFLAGS := -s -w -X github.com/itssoumit/trimdown/internal/meta.Version=$(VERSION)

.PHONY: build test lint vet tidy run crosscheck clean snapshot release-check

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

test:
	go test -race ./...

vet:
	go vet ./...

lint:
	golangci-lint run

tidy:
	go mod tidy

# Prove the no-CGO cross-compile story across the release matrix.
crosscheck:
	@for pair in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64; do \
		os=$${pair%/*}; arch=$${pair#*/}; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -o /dev/null $(PKG) || exit 1; \
	done
	@echo "cross-compile OK"

# Validate the GoReleaser config without publishing.
release-check:
	goreleaser check

# Build all artifacts locally (no publish) to dist/.
snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -f $(BINARY)
	rm -rf dist
