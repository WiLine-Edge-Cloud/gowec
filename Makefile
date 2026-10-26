VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test vet dist clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/gowec ./cmd/gowec

test:
	go test ./...

vet:
	go vet ./...

# A release build of every platform into dist/, the way CI makes it
# (.goreleaser.yaml); publishes nothing.
dist:
	goreleaser release --snapshot --clean

clean:
	rm -rf bin dist
