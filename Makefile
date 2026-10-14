.PHONY: check build test test-fast lint format coverage doc json-gen

SCHEMAS_DIR := $(CURDIR)/afspec/schemas
DOC_DIR     ?= build/docs

VERSION    := $(shell git describe --tags 2>/dev/null || echo "0.1.0")
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -ldflags "\
  -X github.com/agent-fox-dev/spec.Version=$(VERSION) \
  -X github.com/agent-fox-dev/spec.Commit=$(COMMIT) \
  -X github.com/agent-fox-dev/spec.BuildTime=$(BUILD_TIME)"

# Run lint + all tests
check: lint test

# Build the spec and specr commands into build/
build:
	CGO_ENABLED=0 go install $(LDFLAGS) ./cmd/spec
	CGO_ENABLED=0 go install $(LDFLAGS) ./cmd/specr

# Run all tests
test:
	go test ./... -count=1

# Run the fast tests only
test-fast:
	go test ./... -short -count=1

# Run linter
lint:
	test -z "$$(gofmt -l .)"
	go vet ./...

format:
	gofmt -w .

# Generate test coverage
coverage:
	go test ./... -coverprofile=coverage.txt -covermode=atomic
	go tool cover -func=coverage.txt

# Generate the Go package reference (go doc -all) as text files under build/docs
doc:
	@rm -rf $(DOC_DIR)
	@mkdir -p $(DOC_DIR)
	@mod=$$(go list -m); \
	for p in $$(go list ./...); do \
		rel=$${p#$$mod/}; out="$(CURDIR)/$(DOC_DIR)/$$rel.txt"; \
		mkdir -p "$$(dirname "$$out")" && go doc -all "$$p" > "$$out" || exit 1; \
	done
	@echo "package docs written to $(DOC_DIR)"

# Regenerate the Go artifact types from the bundled JSON Schemas.
# The canonical schemas live in specification/schemas/; the copies under
# afspec/schemas/ are what the library compiles and embeds.
# --only-models: structural checking belongs to Validate(), which runs the
# compiled schemas. Generated UnmarshalJSON methods would duplicate it and
# would also reject the empty scaffold CreateSpec writes, which Save and
# LoadSpec must round-trip.
json-gen:
	go install github.com/atombender/go-jsonschema@latest
	go-jsonschema --only-models -p afspec $(SCHEMAS_DIR)/requirements.v2.json  > $(CURDIR)/afspec/requirements.v2.go
	go-jsonschema --only-models -p afspec $(SCHEMAS_DIR)/test_spec.v2.json     > $(CURDIR)/afspec/test_spec.v2.go
	go-jsonschema --only-models -p afspec $(SCHEMAS_DIR)/tasks.v2.json         > $(CURDIR)/afspec/tasks.v2.go
	go-jsonschema --only-models -p afspec $(SCHEMAS_DIR)/prd-frontmatter.v2.json > $(CURDIR)/afspec/prd-frontmatter.v2.go
