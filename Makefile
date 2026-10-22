VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64

.PHONY: build test vet dist clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/gowec ./cmd/gowec

test:
	go test ./...

vet:
	go vet ./...

dist:
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		echo "dist/gowec_$${os}_$${arch}$$ext"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" \
			-o dist/gowec_$${os}_$${arch}$$ext ./cmd/gowec || exit 1; \
	done

clean:
	rm -rf bin dist
