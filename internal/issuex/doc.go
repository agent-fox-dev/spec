// Package issuex is the spec CLI's forge client: it reads a GitHub or GitLab
// issue, pull request or merge request with its comments, which is one of the
// inputs the CLI accepts, and posts a comment back for --comment.
//
// The forge is decided from the URL's host, or, without a URL, from the
// working tree's origin remote. A host that names neither forge is refused
// before any request is sent, so a token never leaves for a host it does not
// belong to. Tokens come from GITHUB_TOKEN or GH_TOKEN, and GITLAB_TOKEN;
// GITHUB_API_URL and GITLAB_API_URL name a self-hosted instance.
package issuex
