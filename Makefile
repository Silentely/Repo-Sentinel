.PHONY: fmt test vet build verify

fmt:
	go fmt ./...

test:
	go test ./...

vet:
	go vet ./...

build:
	go build ./cmd/reposentinel

verify: fmt test vet build
