.PHONY: clean test test-fast lint format check

PYTHON_DIR := $(CURDIR)/packages/afspec

SCHEMAS_DIR := $(CURDIR)/specification/schemas
PYTHON_SCHEMAS_DIR := $(PYTHON_DIR)/afspec/schemas

clean:
	find . -type d -name __pycache__ -exec rm -rf {} + 2>/dev/null || true
	find . -type f -name '*.pyc' -delete 2>/dev/null || true
	find . -type f -name '*.pyo' -delete 2>/dev/null || true
	rm -rf .pytest_cache/ *.egg-info/ dist/ .ruff_cache/ .mypy_cache/ .hypothesis/
	rm -rf packages/*/.pytest_cache packages/*/.mypy_cache packages/*/.ruff_cache
	rm -rf packages/*/build packages/*/dist packages/*/*.egg-info

test:
	uv run pytest -q

test-fast:
	uv run pytest -m "not slow" -q

lint:
	uv run ruff check packages/ && uv run ruff format --check packages/

format:
	uv run ruff format packages/

check: lint test

# Sync the canonical schemas into the Python package.
schema-sync:
	rm -rf $(PYTHON_SCHEMAS_DIR)/*.json && cp $(SCHEMAS_DIR)/*.v1.json $(PYTHON_SCHEMAS_DIR)/
