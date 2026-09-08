# agentspec

AI-powered spec creation library that drives PRD assessment, refinement, and
artifact generation via the Anthropic Claude API. It implements the session
state machine for authoring specs from product requirements documents,
producing structured `requirements.json`, `test_spec.json`, and `tasks.json`
artifacts. Used by the [`spec`](../spec/) CLI.

## Installation

```bash
pip install "agentspec @ git+https://github.com/agent-fox-dev/spec.git@v1.4.1#subdirectory=packages/agentspec"
```

Requires Python 3.12+.

## Quick Start

```python
import asyncio
from pathlib import Path
from agentspec import Campaign, SpecSession


async def main():
    # Create a campaign and add a spec from a PRD
    campaign = Campaign.create(Path("./my-campaign"), "My Campaign", "Description")
    session = campaign.new_spec("user_auth", prd=Path("prd.md"), source=Path("./src"))

    # Assess the PRD -- returns questions for the author
    assessment = await session.assess()
    print(assessment.quality)  # "ready" | "needs_refinement" | "incomplete"
    for q in assessment.questions:
        print(f"[{q.id}] {q.text}")

    # Refine with answers (repeat until assessment.quality == "ready")
    answers = {"q1": "Yes, OAuth2 is required.", "q2": "PostgreSQL."}
    assessment = await session.refine(answers)

    # Accept the PRD and generate artifacts
    session.accept_prd()
    result = await session.generate()
    print(result.artifacts)  # ["requirements", "test_spec", "tasks"]


asyncio.run(main())
```

To resume an existing session:

```python
session = SpecSession.resume(Path(".specs/01_user_auth"))
```

## Key Types

| Type | Description |
|------|-------------|
| `SpecSession` | Manages the full spec lifecycle: assess, refine, generate, validate, render. |
| `Assessment` | Assessment result with `quality` rating, `summary`, `gaps`, and `questions`. |
| `Question` | A refinement question with `id`, `text`, `context`, `options`, and `required` flag. |
| `GenerateResult` | Generation outcome listing produced `artifacts` and optional `warnings`. |
| `ValidationResult` | Validation outcome with `valid` flag and `schema_errors` / `integrity_errors`. |
| `RepairSuggestion` | A suggested repair for a spec artifact, with optional patch. |
| `SessionState` | Enum: `INIT` -> `ASSESSING` -> `REFINING` -> `PRD_ACCEPTED` -> `GENERATING` -> `GENERATED`. |
| `Campaign` | Campaign directory management: create, open, list specs, add new specs. |
| `CampaignMetadata` | Metadata from `campaign.yaml` (name, description, timestamps). |
| `AgentSpecConfig` | Resolved configuration (model, auth method, Vertex AI settings). |
| `load_config()` | Load configuration with precedence: env var -> config file -> defaults. |

### Error Types

All errors inherit from `AgentSpecError`:

| Error | When |
|-------|------|
| `AgentError` | API call failure or unparseable response. Includes `retryable` and `http_status`. |
| `ConfigError` | Invalid configuration or authentication setup. |
| `CampaignError` | Campaign directory operation failure. |
| `SessionError` | Illegal state transition or missing artifacts. |

## Configuration

The model used for AI calls is resolved with the following precedence:

1. `AF_SPEC_MODEL` environment variable (highest priority)
2. `[model]` section in `.specs/config.toml` (project-local) or `~/.specs/config.toml` (global)
3. Hardcoded default (`STANDARD`)

Provider settings (`auth_method`, `vertex_project`, `vertex_region`) are read
from the `[provider]` section in the same config file.

An `ANTHROPIC_API_KEY` environment variable is required for API authentication.

## Requirements

- Python 3.12+
- [afspec](../afspec/) >= 1.4.1
- [anthropic\[bedrock,vertex\]](https://pypi.org/project/anthropic/) >= 0.111
- [PyYAML](https://pypi.org/project/PyYAML/) >= 6.0
