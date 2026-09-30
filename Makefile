BINARY := git-envdiff
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/tejjasdev/git-envdiff/internal/buildinfo.Version=$(VERSION)

.PHONY: build test test-race vet fmt fmt-check lint install-local clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

test:
	go test ./...

test-race:
	go test ./... -race -count=1

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "Not gofmt-formatted:"; echo "$$unformatted"; exit 1; \
	fi

# Everything CI checks, runnable locally before pushing.
lint: fmt-check vet test-race

# Build and install into $GOPATH/bin (or $GOBIN) for local testing, without
# going through install.sh.
install-local: build
	go install -ldflags "$(LDFLAGS)" .

clean:
	rm -f $(BINARY) $(BINARY).exe
