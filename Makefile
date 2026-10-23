.DEFAULT_GOAL := help

DOC_DIR ?= docs/generated

# agentkit-go has no tags, so go.mod pins a pseudo-version of its main branch.
# Every target that compiles first moves that pin to the latest main, so the
# repository always builds against the latest agentkit-go (see ADR 06).
# AGENTKIT_UPDATE=0 skips the update: offline, or to build the pinned version.
AGENTKIT        := github.com/agent-fox-dev/agentkit-go
AGENTKIT_UPDATE ?= 1
AGENTKIT_DEP    := $(if $(filter 1,$(AGENTKIT_UPDATE)),update-agentkit)

VERSION    := $(shell git describe --tags 2>/dev/null || echo "0.1.0")
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -ldflags "\
  -X github.com/agent-fox-dev/spec.Version=$(VERSION) \
  -X github.com/agent-fox-dev/spec.Commit=$(COMMIT) \
  -X github.com/agent-fox-dev/spec.BuildTime=$(BUILD_TIME)"

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*##"}; {printf "  %-16s %s\n", $$1, $$2}'

.PHONY: build
build: $(AGENTKIT_DEP) ## Install the spec, specr and speca commands
	CGO_ENABLED=0 go install $(LDFLAGS) ./cmd/spec
	CGO_ENABLED=0 go install $(LDFLAGS) ./cmd/specr
	CGO_ENABLED=0 go install $(LDFLAGS) ./cmd/speca

.PHONY: test
test: $(AGENTKIT_DEP) ## Run all Go tests
	go test ./... -count=1

.PHONY: test-fast
test-fast: $(AGENTKIT_DEP) ## Run the fast tests only
	go test ./... -short -count=1

.PHONY: coverage
coverage: $(AGENTKIT_DEP) ## Run all tests with a coverage report
	go test ./... -coverprofile=coverage.txt -covermode=atomic
	go tool cover -func=coverage.txt

.PHONY: fmt
fmt: ## gofmt all source files in place
	gofmt -l -w .

.PHONY: vet
vet: $(AGENTKIT_DEP) ## go vet ./...
	go vet ./...

.PHONY: lint
lint: $(AGENTKIT_DEP) ## Run golangci-lint if installed, otherwise skip
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed; skipping (see https://golangci-lint.run)"; \
	fi

.PHONY: check
check: fmt vet lint test ## Run fmt, vet, lint and test - use before committing

.PHONY: doc
doc: $(AGENTKIT_DEP) ## Generate Go package reference (go doc -all) as text files under docs/generated
	@rm -rf $(DOC_DIR)
	@mkdir -p $(DOC_DIR)
	@mod=$$(go list -m); \
	for p in $$(go list -f '{{if .GoFiles}}{{.ImportPath}}{{end}}' ./... | grep -v -e /internal/ -e /examples/); do \
		rel=$${p#$$mod}; rel=$${rel#/}; out="$(CURDIR)/$(DOC_DIR)/$${rel:-$$(basename $$mod)}.txt"; \
		mkdir -p "$$(dirname "$$out")" && go doc -all "$$p" > "$$out" || exit 1; \
	done
	@echo "package docs written to $(DOC_DIR)"

.PHONY: update-agentkit
update-agentkit: ## Point go.mod at the latest agentkit-go main (needs network)
	@rev=$$(go list -m -f '{{.Version}}' $(AGENTKIT)@main) || { \
		echo "cannot resolve $(AGENTKIT)@main; offline? run make with AGENTKIT_UPDATE=0" >&2; exit 1; }; \
	go mod edit -replace=$(AGENTKIT)@v0.0.0=$(AGENTKIT)@$$rev -require=$(AGENTKIT)/codesearch@$$rev && \
	go mod tidy && echo "agentkit-go: $$rev"

.PHONY: tidy
tidy: ## go mod tidy
	go mod tidy

.PHONY: clean
clean: ## Remove build output and the coverage profile
	rm -rf build coverage.txt

# schemas

SCHEMAS_DIR := $(CURDIR)/afspec/schemas

# Regenerate the Go artifact types from the bundled JSON Schemas.
# The canonical schemas live in specification/schemas/; the copies under
# afspec/schemas/ are what the library compiles and embeds.
# --only-models: structural checking belongs to Validate(), which runs the
# compiled schemas. Generated UnmarshalJSON methods would duplicate it and
# would also reject the empty scaffold CreateSpec writes, which Save and
# LoadSpec must round-trip.
.PHONY: json-gen
json-gen: ## Regenerate the Go artifact types from the JSON Schemas
	go install github.com/atombender/go-jsonschema@latest
	go-jsonschema --only-models -p afspec $(SCHEMAS_DIR)/requirements.v2.json  > $(CURDIR)/afspec/requirements.v2.go
	go-jsonschema --only-models -p afspec $(SCHEMAS_DIR)/test_spec.v2.json     > $(CURDIR)/afspec/test_spec.v2.go
	go-jsonschema --only-models -p afspec $(SCHEMAS_DIR)/tasks.v2.json         > $(CURDIR)/afspec/tasks.v2.go
	go-jsonschema --only-models -p afspec $(SCHEMAS_DIR)/prd-frontmatter.v2.json > $(CURDIR)/afspec/prd-frontmatter.v2.go

# manage skills

SKILLS_TEMPLATES_DIR := $(CURDIR)/skills
CLAUDE_SKILLS_DIR := $(HOME)/.claude/skills

.PHONY: install-skills uninstall-skills
install-skills: ## Install the af-* skills under skills/ into ~/.claude/skills
	@for skill in $(SKILLS_TEMPLATES_DIR)/af-*; do \
		name=$$(basename "$$skill"); \
		target="$(CLAUDE_SKILLS_DIR)/$$name"; \
		mkdir -p "$$target"; \
		cp "$$skill" "$$target/SKILL.md"; \
		echo "installed: $$name -> $$target/SKILL.md"; \
	done

uninstall-skills: ## Remove the installed af-* skills from ~/.claude/skills
	@for skill in $(SKILLS_TEMPLATES_DIR)/af-*; do \
		name=$$(basename "$$skill"); \
		if [ -d "$(CLAUDE_SKILLS_DIR)/$$name" ]; then \
			rm -rf "$(CLAUDE_SKILLS_DIR)/$$name"; \
			echo "removed: $$name"; \
		fi; \
	done
