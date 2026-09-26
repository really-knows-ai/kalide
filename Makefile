# Makefile — THE single build entry point for eypres. Later phases extend this
# same file (phase 8 adds a `release` target); there is no second build script.

GO      ?= go
BINARY  := eypres
DIST    := dist
# VERSION is the version stamped into release binaries via -ldflags. The
# release workflow sets it from the pushed vX.Y.Z tag; local builds default to
# "dev". It targets the `version` variable in cmd/eypres.
VERSION ?= dev

# Binaries are CGO-free per global.constraint.go-static-embedded-binary. Only
# the supported targets are ever built: darwin/arm64 and windows/amd64 are
# required, windows/arm64 is built while its native runner is available. Never
# darwin/amd64 or Linux.

.PHONY: all fmt vet build test cross release clean

# Default target: format, vet, build and test.
all: fmt vet build test

# gofmt the module. `make fmt` rewrites; the gate check is `gofmt -l .`.
fmt:
	gofmt -w .

vet:
	$(GO) vet ./...

build:
	$(GO) build ./...

test:
	$(GO) test ./...

# CGO_ENABLED=0 cross-compile for every supported target into dist/.
cross:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build -o $(DIST)/$(BINARY)-darwin-arm64 ./cmd/eypres
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -o $(DIST)/$(BINARY)-windows-amd64.exe ./cmd/eypres
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 $(GO) build -o $(DIST)/$(BINARY)-windows-arm64.exe ./cmd/eypres

# Release build: static (CGO_ENABLED=0) binaries with a reproducible -trimpath
# and the version stamped in via -ldflags, for exactly the supported targets:
# darwin/arm64, windows/amd64 (eypres.exe) and windows/arm64 (eypres.exe;
# built/released only while its native runner exists). Never darwin/amd64 or
# Linux. This is the target the phase-8 gate cross-compile and the phase-9
# release.yml build job invoke.
release:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o $(DIST)/$(BINARY)-darwin-arm64 ./cmd/eypres
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o $(DIST)/$(BINARY)-windows-amd64.exe ./cmd/eypres
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 $(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o $(DIST)/$(BINARY)-windows-arm64.exe ./cmd/eypres

clean:
	rm -rf $(DIST)
