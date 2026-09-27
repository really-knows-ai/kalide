# Makefile — THE single build entry point for kalide. Later phases extend this
# same file (phase 8 adds a `release` target); there is no second build script.

GO      ?= go
BINARY  := kalide
DIST    := dist
# VERSION is the version stamped into release binaries via -ldflags. The
# release workflow sets it from the pushed vX.Y.Z tag; local builds default to
# "dev". It targets the `version` variable in cmd/kalide.
VERSION ?= dev

# Binaries are CGO-free per global.constraint.go-static-embedded-binary. All
# six supported targets are built: darwin/arm64, darwin/amd64,
# windows/amd64, windows/arm64, linux/amd64 and linux/arm64. Each is built
# and released only while its CI runner is available (native-e2e.yml is the
# runner-gated source of truth); a target degrades gracefully out of the
# release if its runner disappears.

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
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build -o $(DIST)/$(BINARY)-darwin-arm64 ./cmd/kalide
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build -o $(DIST)/$(BINARY)-darwin-amd64 ./cmd/kalide
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -o $(DIST)/$(BINARY)-windows-amd64.exe ./cmd/kalide
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 $(GO) build -o $(DIST)/$(BINARY)-windows-arm64.exe ./cmd/kalide
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -o $(DIST)/$(BINARY)-linux-amd64 ./cmd/kalide
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -o $(DIST)/$(BINARY)-linux-arm64 ./cmd/kalide

# Release build: static (CGO_ENABLED=0) binaries with a reproducible -trimpath
# and the version stamped in via -ldflags, for all six supported targets:
# darwin/arm64, darwin/amd64, windows/amd64 (kalide.exe), windows/arm64
# (kalide.exe), linux/amd64 and linux/arm64. Each target is built and
# released only while its native CI runner exists; this is the target the
# phase-8 gate cross-compile and the phase-9 release.yml build job invoke.
release:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o $(DIST)/$(BINARY)-darwin-arm64 ./cmd/kalide
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o $(DIST)/$(BINARY)-darwin-amd64 ./cmd/kalide
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o $(DIST)/$(BINARY)-windows-amd64.exe ./cmd/kalide
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 $(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o $(DIST)/$(BINARY)-windows-arm64.exe ./cmd/kalide
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o $(DIST)/$(BINARY)-linux-amd64 ./cmd/kalide
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o $(DIST)/$(BINARY)-linux-arm64 ./cmd/kalide

clean:
	rm -rf $(DIST)
