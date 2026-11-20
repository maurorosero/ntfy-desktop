# SPDX-License-Identifier: GPL-3.0-or-later
#
# El toolchain vive en .toolchain/go: no hace falta Go instalado en el sistema.

GO      ?= $(CURDIR)/.toolchain/go/bin/go
BINARIO ?= bin/ntfy-desktop
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/maurorosero/ntfy-desktop/internal/version.Version=$(VERSION)

.PHONY: bootstrap fmt vet test vuln build run check clean

bootstrap:
	@./tools/bootstrap-go.sh

fmt: bootstrap
	@$(GO) fmt ./...

vet: bootstrap
	@$(GO) vet ./...

test: bootstrap
	@$(GO) test ./...

vuln: bootstrap
	@$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...

build: bootstrap
	@mkdir -p bin
	@CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARIO) ./cmd/ntfy-desktop

run: build
	@$(BINARIO)

check: fmt vet test build

clean:
	@rm -rf bin dist coverage.out
