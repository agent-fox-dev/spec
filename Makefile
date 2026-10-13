.PHONY: check test

VERSION    := $(shell git describe --tags 2>/dev/null || echo "0.1.0")
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

SCHEMAS_DIR := $(CURDIR)/afspec/schemas

LDFLAGS := -ldflags "\
  -X github.com/agent-fox-dev/agentfox.Version=$(VERSION) \
  -X github.com/agent-fox-dev/agentfox.Commit=$(COMMIT) \
  -X github.com/agent-fox-dev/agentfox.BuildTime=$(BUILD_TIME)"

# Run lint + all tests
check: lint test

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

# Regenerate the Go artifact types from the bundled JSON Schemas.
# The canonical schemas live in specification/schemas/; the copies under
# afspec/schemas/ are what the library compiles and embeds.
# --only-models: structural checking belongs to Validate(), which runs the
# compiled schemas. Generated UnmarshalJSON methods would duplicate it and
# would also reject a scaffold that `spec new` must be able to write and read.
json-gen:
	go install github.com/atombender/go-jsonschema@latest
	go-jsonschema --only-models -p afspec $(SCHEMAS_DIR)/requirements.v2.json  > $(CURDIR)/afspec/requirements.v2.go
	go-jsonschema --only-models -p afspec $(SCHEMAS_DIR)/test_spec.v2.json     > $(CURDIR)/afspec/test_spec.v2.go
	go-jsonschema --only-models -p afspec $(SCHEMAS_DIR)/tasks.v2.json         > $(CURDIR)/afspec/tasks.v2.go
	go-jsonschema --only-models -p afspec $(SCHEMAS_DIR)/prd-frontmatter.v2.json > $(CURDIR)/afspec/prd-frontmatter.v2.go