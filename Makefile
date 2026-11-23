BINARY  := headquarters
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64
# Coverage counts code exercised by the tests of any package, except the test
# helpers themselves.
COVERPKG = $(shell go list ./... | grep -v /internal/testutil | paste -sd, -)

.PHONY: build install test test-installer test-race coverage perf lint tag release installer clean

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/headquarters

install:
	go install -trimpath -ldflags '$(LDFLAGS)' ./cmd/headquarters

test:
	go test ./...

test-installer:
	+sh scripts/test-installer.sh

test-race:
	go test -race ./...

# Runs the tests with the race detector, writes coverage.out, and prints a
# Markdown report.
coverage:
	go test -race -covermode=atomic -coverpkg=$(COVERPKG) -coverprofile=coverage.out ./...
	@sh scripts/coverage-summary.sh coverage.out

perf:
	HEADQUARTERS_PERF=1 go test -count=1 -run TestInspectionPerformance -v ./internal/app/

lint:
	go vet ./...
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)

tag: export RELEASE_TAG := $(value TAG)
tag:
	@if ! printf '%s\n' "$$RELEASE_TAG" | LC_ALL=C awk 'NR == 1 && /^v[0-9]+\.[0-9]+\.[0-9]+$$/ { valid = 1 } END { exit !(NR == 1 && valid) }'; then \
		printf '%s\n' 'Usage: make tag TAG=vMAJOR.MINOR.PATCH' >&2; \
		exit 1; \
	fi
	git tag -a -m "$(BINARY) $$RELEASE_TAG" -- "$$RELEASE_TAG"

release: clean
	@mkdir -p dist
	@set -e; for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; name=$(BINARY)_$(VERSION)_$${os}_$${arch}; \
		echo "building $$name"; \
		mkdir -p dist/$$name; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o dist/$$name/$(BINARY) ./cmd/headquarters || exit 1; \
		cp LICENSE README.md examples/config.toml dist/$$name/; \
		tar -C dist -czf dist/$$name.tar.gz $$name; \
		rm -rf dist/$$name; \
	done
	@cd dist && (command -v sha256sum >/dev/null && sha256sum *.tar.gz || shasum -a 256 *.tar.gz) > checksums.txt
	@cat dist/checksums.txt
	@$(MAKE) --no-print-directory installer > dist/install.sh

installer:
	@sed 's/@VERSION@/$(VERSION)/' scripts/install.sh

clean:
	rm -rf bin dist
