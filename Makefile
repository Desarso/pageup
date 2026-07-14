VERSION ?= dev
GOFLAGS ?=

.PHONY: all build test test-race vet check install docker clean

all: check build

build:
	mkdir -p bin
	go build $(GOFLAGS) -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o bin/pageup ./cmd/pageup
	go build $(GOFLAGS) -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o bin/pageup-server ./cmd/pageup-server

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

check: test test-race vet

install: build
	mkdir -p $(HOME)/.local/bin
	install -m 0755 bin/pageup $(HOME)/.local/bin/pageup

docker:
	docker build --build-arg VERSION=$(VERSION) -t pageup:$(VERSION) .

clean:
	rm -rf bin
