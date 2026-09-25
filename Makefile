# Builds need only Go (or Docker: make docker-test / docker-dist).
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64
GO_IMAGE := golang:1.26-alpine
DOCKER_GO := docker run --rm -v "$(CURDIR)":/src -w /src -e GOFLAGS=-buildvcs=false $(GO_IMAGE)

.PHONY: build test dist docker-test docker-dist
build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/cw .

test:
	test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)
	go vet ./...
	go test ./...

dist:
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		echo "dist/cw_$${os}_$${arch}$$ext"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" -o dist/cw_$${os}_$${arch}$$ext . || exit 1; \
	done

docker-test:
	$(DOCKER_GO) sh -c "apk add -q make git && make test"

docker-dist:
	$(DOCKER_GO) sh -c "apk add -q make git && make dist VERSION=$(VERSION)"
