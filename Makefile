VERSION ?= $(shell git describe --tags --match 'v*' --always --dirty 2>/dev/null || echo dev)
PREFIX ?= $(HOME)/.local

.PHONY: build install check
build:
	CGO_ENABLED=1 go build -ldflags '-X main.version=$(VERSION)' -o lotel ./cmd/lotel

install: build
	mkdir -p "$(DESTDIR)$(PREFIX)/bin"
	install -m 755 lotel "$(DESTDIR)$(PREFIX)/bin/lotel"

check:
	go test ./...
	go build ./...
	go vet ./...
	python3 distribution/tests/run_tests.py
