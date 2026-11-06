# architecture.md

What `af-spec` §8 writes, when it writes it at all: when the file is worth
having, its sections, and the two constraints on it.

Write it only when the person asks, or when the PRD carries design detail
that is neither intent nor a criterion: modules and their responsibilities,
the interfaces between them with real signatures, the data models, the
technology choices and their reasons. Sections: `# Architecture: {spec_name}`,
`## Overview`, `## Architecture` (a Mermaid flowchart, then one line of
responsibility per module), `## Components and Interfaces`, `## Data
Models`, `## Technology Stack`, `## Definition of Done`.

Two constraints. It fits the code that exists — the project's module names,
vocabulary and libraries. And **nothing in it is load-bearing**: a renderer
working to a token budget drops it first, so every fact a coder needs to
implement belongs in a criterion or a task step. It has no frontmatter and is
not validated.
