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

gofmt-check:
	@test -z "$$(gofmt -l cmd internal migrations)" || (echo "Run 'gofmt -w' on:"; gofmt -l cmd internal migrations; exit 1)

.PHONY: fmt test vet lint build build-production verify test-frontend docs-dev docs-build gofmt-check

fmt:
	go fmt ./...

test:
	# 与 CI 一致跑 race（本地 verify 不复现 CI 失败的常见原因）。
	go test -race ./...

test-frontend:
	pnpm --dir web typecheck && pnpm --dir web test -- --run

vet:
	go vet ./...

lint:
	golangci-lint run ./...

build:
	go build -ldflags "$(BUILD_LDFLAGS)" -o "$(OUTPUT)" ./cmd/reposentinel

build-production:
	go build -tags production -ldflags "$(BUILD_LDFLAGS)" -o "$(OUTPUT)" ./cmd/reposentinel

docs-dev:
	npm run docs:dev

docs-build:
	npm run docs:build

verify: gofmt-check fmt test vet build test-frontend
