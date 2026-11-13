.DEFAULT_GOAL := help

DOC_DIR ?= docs/generated

# go.mod pins a released agentkit-go and carries no replace, so every target
# builds the committed pin and none of them needs the network (see ADR 13).
# `make update-agentkit` moves the pin, by hand, when somebody wants it moved.
AGENTKIT := github.com/agent-fox-dev/agentkit-go

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

# specrun is built with the SHA-256 of the af-impl SKILL.md as this tree
# renders it for Claude Code, so that a run can tell the installed skill from
# another (ADR 23). Recursive, so that only build renders it.
SKILL_SHA256 = $(shell go run ./internal/skillgen --source skills --sha256 af-impl)
SPECRUN_LDFLAGS = -ldflags "\
  -X github.com/agent-fox-dev/spec.Version=$(VERSION) \
  -X github.com/agent-fox-dev/spec.Commit=$(COMMIT) \
  -X github.com/agent-fox-dev/spec.BuildTime=$(BUILD_TIME) \
  -X github.com/agent-fox-dev/spec/internal/specrun.builtSkillSHA256=$(SKILL_SHA256)"

.PHONY: build
build: ## Install the spec, specr, speca and specrun commands
	CGO_ENABLED=0 go install $(LDFLAGS) ./cmd/spec
	CGO_ENABLED=0 go install $(LDFLAGS) ./cmd/specr
	CGO_ENABLED=0 go install $(LDFLAGS) ./cmd/speca
	@test -n "$(SKILL_SHA256)" || { echo "make build: the af-impl render failed; specrun would carry no skill hash" >&2; exit 1; }
	CGO_ENABLED=0 go install $(SPECRUN_LDFLAGS) ./cmd/specrun

.PHONY: test
test: ## Run all Go tests
	go test ./... -count=1

.PHONY: test-fast
test-fast: ## Run the fast tests only
	go test ./... -short -count=1

.PHONY: coverage
coverage: ## Run all tests with a coverage report
	go test ./... -coverprofile=coverage.txt -covermode=atomic
	go tool cover -func=coverage.txt

.PHONY: fmt
fmt: ## gofmt all source files in place
	gofmt -l -w .

.PHONY: vet
vet: ## go vet ./...
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint if installed, otherwise skip
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed; skipping (see https://golangci-lint.run)"; \
	fi

.PHONY: check
check: fmt vet lint test skills-check ## Run fmt, vet, lint, test and skills-check - use before committing

.PHONY: doc
doc: ## Generate Go package reference (go doc -all) as text files under docs/generated
	@rm -rf $(DOC_DIR)
	@mkdir -p $(DOC_DIR)
	@mod=$$(go list -m); \
	for p in $$(go list -f '{{if .GoFiles}}{{.ImportPath}}{{end}}' ./... | grep -v -e /internal/ -e /examples/); do \
		rel=$${p#$$mod}; rel=$${rel#/}; out="$(CURDIR)/$(DOC_DIR)/$${rel:-$$(basename $$mod)}.txt"; \
		mkdir -p "$$(dirname "$$out")" && go doc -all "$$p" > "$$out" || exit 1; \
	done
	@echo "package docs written to $(DOC_DIR)"

.PHONY: update-agentkit
update-agentkit: ## Move the agentkit-go pin to its latest release (needs network)
	@go get $(AGENTKIT)@latest || { \
		echo "cannot resolve $(AGENTKIT)@latest; offline?" >&2; exit 1; }
	@go mod tidy
	@echo "agentkit-go: $$(go list -m -f '{{.Version}}' $(AGENTKIT))"
	@echo "note: $(AGENTKIT)/codesearch has no tags; it stays on the pseudo-version"
	@echo "      in go.mod and is moved by hand (see ADR 13)."

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
	go-jsonschema --only-models -p afspec $(SCHEMAS_DIR)/requirements.v3.json  > $(CURDIR)/afspec/requirements.v3.go
	go-jsonschema --only-models -p afspec $(SCHEMAS_DIR)/progress.v3.json      > $(CURDIR)/afspec/progress.v3.go
	go-jsonschema --only-models -p afspec $(SCHEMAS_DIR)/tasks.v3.json         > $(CURDIR)/afspec/tasks.v3.go
	go-jsonschema --only-models -p afspec $(SCHEMAS_DIR)/prd-frontmatter.v3.json > $(CURDIR)/afspec/prd-frontmatter.v3.go

# render and install the skills

# The render reads SKILLS_SRC and writes SKILLS_OUT. It imports only the
# standard library and go-yaml, so none of these targets depends on
# no target at all now: the render reaches no network and reads no credential,
# and neither does any other target (ADR 13).
SKILLS_SRC ?= skills
SKILLS_OUT ?= build/skills
SKILLGEN   := go run ./internal/skillgen --source $(SKILLS_SRC)

# AGENT is the agent to install for: claude, codex, cursor, copilot, pi or
# opencode.
# PROJECT installs into that repository instead of the user's directory, and
# SKILLS_DIR overrides both. See docs/configuration.md.
AGENT ?= claude
SKILLS_FLAGS := --agent $(AGENT) \
  $(if $(PROJECT),--project $(PROJECT)) $(if $(SKILLS_DIR),--skills-dir $(SKILLS_DIR))

.PHONY: skills
skills: ## Render the skills under skills/ for every agent into build/skills
	$(SKILLGEN) --out $(SKILLS_OUT)

.PHONY: skills-check
skills-check: ## Check the skills under skills/ against every render rule
	$(SKILLGEN) --check

.PHONY: install-skills
install-skills: ## Install the skills for one agent (AGENT, PROJECT, SKILLS_DIR)
	$(SKILLGEN) --install $(SKILLS_FLAGS)

.PHONY: uninstall-skills
uninstall-skills: ## Remove the installed skills of one agent (AGENT, PROJECT, SKILLS_DIR)
	$(SKILLGEN) --uninstall $(SKILLS_FLAGS)
