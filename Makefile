VERSION := $(shell tr -d '[:space:]' < VERSION)
GIT_SHA ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || printf 'unknown')
GIT_BRANCH ?= $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || printf 'unknown')
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILD_CHANNEL ?= local
OUTPUT ?= reposentinel
BUILD_LDFLAGS := \
	-X github.com/Silentely/Repo-Sentinel/internal/buildinfo.version=$(VERSION) \
	-X github.com/Silentely/Repo-Sentinel/internal/buildinfo.gitSHA=$(GIT_SHA) \
	-X github.com/Silentely/Repo-Sentinel/internal/buildinfo.gitBranch=$(GIT_BRANCH) \
	-X github.com/Silentely/Repo-Sentinel/internal/buildinfo.buildTime=$(BUILD_TIME) \
	-X github.com/Silentely/Repo-Sentinel/internal/buildinfo.buildChannel=$(BUILD_CHANNEL)

.PHONY: fmt test vet build build-production verify

fmt:
	go fmt ./...

test:
	go test ./...

vet:
	go vet ./...

build:
	go build -ldflags "$(BUILD_LDFLAGS)" -o "$(OUTPUT)" ./cmd/reposentinel

build-production:
	go build -tags production -ldflags "$(BUILD_LDFLAGS)" -o "$(OUTPUT)" ./cmd/reposentinel

verify: fmt test vet build
