VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/kunchenguid/no-mistakes/internal/buildinfo.Version=$(VERSION) \
           -X github.com/kunchenguid/no-mistakes/internal/buildinfo.Commit=$(COMMIT) \
           -X github.com/kunchenguid/no-mistakes/internal/buildinfo.Date=$(DATE)

.PHONY: build dist install test e2e lint fmt clean docs docs-build docs-preview skill skill-check

DIST_DIR ?= dist
INSTALL_BIN := $(shell go env GOPATH)/bin/no-mistakes

build:
	go build -buildvcs=false -ldflags "$(LDFLAGS)" -o bin/no-mistakes ./cmd/no-mistakes

dist:
	rm -rf $(DIST_DIR)
	mkdir -p $(DIST_DIR)
	for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do \
		os=$${target%/*}; \
		arch=$${target#*/}; \
		bin=no-mistakes; \
		out="$(DIST_DIR)/$$bin"; \
		if [ "$$os" = "windows" ]; then \
			bin="$$bin.exe"; \
			out="$(DIST_DIR)/$$bin"; \
		fi; \
		CGO_ENABLED=0 GOOS="$$os" GOARCH="$$arch" go build -buildvcs=false -ldflags "$(LDFLAGS)" -o "$$out" ./cmd/no-mistakes; \
		if [ "$$os" = "windows" ]; then \
			( cd "$(DIST_DIR)" && zip -q "no-mistakes-$(VERSION)-$$os-$$arch.zip" "$$bin" ); \
		else \
			tar -C "$(DIST_DIR)" -czf "$(DIST_DIR)/no-mistakes-$(VERSION)-$$os-$$arch.tar.gz" "$$bin"; \
		fi; \
		rm -f "$$out"; \
	done

install: build
	mkdir -p $(dir $(INSTALL_BIN))
	install -m 755 bin/no-mistakes $(INSTALL_BIN)

test:
	go test -race ./...

e2e:
	go test ./cmd/no-mistakes ./internal/guard ./internal/delivery ./internal/legacycleanup

# Regenerate the committed agent skill (skills/no-mistakes/SKILL.md) from the
# internal/skill source of truth.
skill:
	go run ./cmd/genskill

# Fail if the committed skill has drifted from the generator. Wired into lint
# so CI catches a forgotten `make skill`.
skill-check:
	go run ./cmd/genskill --check

lint: skill-check
	go vet ./...

fmt:
	gofmt -w .

docs: docs-build

docs-build:
	cd docs && npm ci --no-audit --no-fund && npm run build

docs-preview:
	cd docs && npm run preview

clean:
	rm -rf bin/
