# golibc has no cgo and no third-party dependencies; everything builds with CGO_ENABLED=0.
# The library is Linux-only, so tests run in Docker when you are on another OS.
export CGO_ENABLED = 0
GO_IMAGE = golang:1.23

.PHONY: help check vet test test-amd64 test-arm64 bench demo golden

help: ## Show this help
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

check: vet test ## Format check, vet and tests on both architectures

vet: ## gofmt and go vet for linux/amd64 and linux/arm64
	gofmt -l . | (! grep .)
	GOOS=linux GOARCH=amd64 go vet ./...
	GOOS=linux GOARCH=arm64 go vet ./...

define docker_test
docker run --rm --platform linux/$(1) -e CGO_ENABLED=0 -v "$(CURDIR)":/src -w /src $(GO_IMAGE) go test -count=1 ./...
endef

test: test-arm64 test-amd64 ## Run the tests in Linux containers (both architectures)

test-arm64: ## Run the tests in a linux/arm64 container
	$(call docker_test,arm64)

test-amd64: ## Run the tests in a linux/amd64 container
	$(call docker_test,amd64)

bench: ## Run the allocator benchmarks in a Linux container
	docker run --rm -e CGO_ENABLED=0 -v "$(CURDIR)":/src -w /src $(GO_IMAGE) go test -run xxx -bench . -cpu 1,4 ./pkg/golibc

demo: ## Run the hello example in an empty (scratch) container
	mkdir -p bin
	GOOS=linux GOARCH=$(shell go env GOARCH) go build -o bin/hello ./examples/hello
	printf 'FROM scratch\nCOPY hello /hello\nENTRYPOINT ["/hello"]\n' > bin/Dockerfile
	docker build -q -t golibc-hello bin >/dev/null && docker run --rm golibc-hello

golden: ## Regenerate the printf and scanf golden files from real glibc (needs Docker)
	scripts/gen_golden.sh
