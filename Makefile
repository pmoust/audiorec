.PHONY: build test lint clean

GO ?= go
BIN := audiorec
PKG := ./...

build:
	$(GO) build -o $(BIN) ./cmd/audiorec

test:
	$(GO) test -race -v $(PKG)

test-short:
	$(GO) test -race -short $(PKG)

lint:
	$(GO) vet $(PKG)

clean:
	rm -rf $(BIN) dist/
