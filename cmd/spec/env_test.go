package main

import "testing"

// cleanEnv empties, for the test's duration, every variable through which a
// model or forge credential, a model choice or a spec root can be found, so a
// test of a refusal sees an environment that has none whatever the machine
// carries. A test then sets exactly the ones it means to.
func cleanEnv(t testing.TB) {
	t.Helper()
	for _, v := range []string{
		"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_OAUTH_TOKEN", "ANTHROPIC_BASE_URL",
		"CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_BEDROCK",
		"ANTHROPIC_VERTEX_PROJECT_ID", "ANTHROPIC_VERTEX_BASE_URL",
		"GITHUB_TOKEN", "GH_TOKEN", "GITHUB_API_URL", "GITLAB_TOKEN", "GITLAB_API_URL",
		"AF_MODEL", "AGENTKIT_MODEL", "AF_MODEL_EFFORT", "AF_SPEC_DIR",
	} {
		t.Setenv(v, "")
	}
}
