package specgen

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/schema"

	"github.com/agent-fox-dev/spec/afspec"
)

// PatchToolName is the tool that corrects one generation step's last
// rejected submission in place.
func PatchToolName(step afspec.GenerationStep) string { return "patch_" + string(step) }

// patchOp is one operation of a patch: RFC 6902's add, remove and replace,
// with the value carried as JSON text.
type patchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value string `json:"value"`
}

// patchArtifactTool builds the step's patch tool.
//
// A rejection names a handful of fields, and the alternative to patching them
// is the model writing the whole artifact again: for a tasks artifact that is
// tens of thousands of output tokens, the slowest and most expensive part of
// a phase, spent to change a few lines. The patch applies to the submission
// the step last turned back, and the result goes through exactly the checks
// a submission does, so a patched artifact is accepted only if a submitted
// one with the same content would be.
//
// The value is JSON text in a string rather than a JSON value: strict mode
// cannot declare a field of any type, and a union of every type would spend
// the request's small budget of union parameters.
func patchArtifactTool(step afspec.GenerationStep, partial *afspec.PartialSpec, sink *artifactSink,
	audit func(map[string]any) error) core.Tool {

	name := PatchToolName(step)
	return core.Tool{
		Name: name,
		Description: fmt.Sprintf("Correct your last rejected %s submission in place, instead of submitting "+
			"the whole artifact again. The operations are applied in order to the artifact as you submitted it "+
			"(with the glossary as an object from term to definition), and the result is validated like a "+
			"submission: accepted, it ends this phase; rejected, it becomes the submission the next patch "+
			"applies to. With no operations, the last submission is checked again as it is.", step),
		InputSchema: schema.Object(
			schema.Prop("operations", schema.Array(schema.Object(
				schema.Prop("op", schema.Enum("add, remove or replace, as in RFC 6902. add inserts into an "+
					"array at the index (or appends at \"-\") and sets an object member; replace needs the "+
					"target to exist.", "add", "remove", "replace")),
				schema.Prop("path", schema.String("JSON pointer into the artifact, e.g. "+
					"/requirements/0/criteria/2/action.")),
				schema.Prop("value", schema.String("The new value as JSON text: a string is quoted, "+
					"e.g. \"\\\"return HTTP 404\\\"\", an object is {...}. Empty for remove.")),
			), "The operations, applied in order.")),
		),
		Terminating: true,
		Execute: func(_ context.Context, in json.RawMessage) core.ToolResult {
			var payload struct {
				Operations []patchOp `json:"operations"`
			}
			if err := json.Unmarshal(in, &payload); err != nil {
				return core.ErrResult("invalid_arguments", err.Error())
			}
			held := sink.heldSubmission()
			if held == nil {
				return core.ErrResult("nothing_to_patch", fmt.Sprintf(
					"there is no rejected %s submission to patch; call %s with the whole artifact.",
					step, ArtifactToolName(step)))
			}
			patched, err := applyPatch(held, payload.Operations)
			if err != nil {
				return core.ErrResult("patch_failed", fmt.Sprintf(
					"%v. No operation was applied; the last submission is unchanged.", err))
			}
			return settleArtifact(patched, step, partial, sink, audit)
		},
	}
}

// applyPatch applies ops, in order, to a copy of doc. Either every operation
// applies or the error names the first that did not, and doc is untouched.
func applyPatch(doc map[string]any, ops []patchOp) (map[string]any, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	for i, op := range ops {
		if root, err = applyOp(root, op); err != nil {
			return nil, fmt.Errorf("operation %d (%s %s): %w", i+1, op.Op, op.Path, err)
		}
	}
	out, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("the patched artifact is not a JSON object")
	}
	return out, nil
}

func applyOp(root any, op patchOp) (any, error) {
	tokens, err := pointerTokens(op.Path)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("the path names the whole artifact; name a field inside it")
	}
	var value any
	if op.Op == "add" || op.Op == "replace" {
		if err := json.Unmarshal([]byte(op.Value), &value); err != nil {
			return nil, fmt.Errorf("the value is not JSON text (%v); a string value is quoted", err)
		}
	} else if op.Op != "remove" {
		return nil, fmt.Errorf("unknown op %q; use add, remove or replace", op.Op)
	}
	parent, err := resolve(root, tokens[:len(tokens)-1])
	if err != nil {
		return nil, err
	}
	last := tokens[len(tokens)-1]
	switch p := parent.container.(type) {
	case map[string]any:
		_, exists := p[last]
		if op.Op != "add" && !exists {
			return nil, fmt.Errorf("there is no member %q", last)
		}
		if op.Op == "remove" {
			delete(p, last)
		} else {
			p[last] = value
		}
		return root, nil
	case []any:
		idx, err := arrayIndex(last, len(p), op.Op == "add")
		if err != nil {
			return nil, err
		}
		var next []any
		switch op.Op {
		case "add":
			next = append(append(append([]any{}, p[:idx]...), value), p[idx:]...)
		case "remove":
			next = append(append([]any{}, p[:idx]...), p[idx+1:]...)
		default:
			next = append([]any{}, p...)
			next[idx] = value
		}
		return parent.set(root, next), nil
	default:
		return nil, fmt.Errorf("%s is neither an object nor an array", pointerOf(tokens[:len(tokens)-1]))
	}
}

// located is a container reached by a pointer, with a way to replace it: an
// array cannot grow or shrink in place.
type located struct {
	container any
	set       func(root, replacement any) any
}

func resolve(root any, tokens []string) (located, error) {
	cur := located{container: root, set: func(_, r any) any { return r }}
	for i, tok := range tokens {
		switch c := cur.container.(type) {
		case map[string]any:
			v, ok := c[tok]
			if !ok {
				return located{}, fmt.Errorf("there is no member %q at %s", tok, pointerOf(tokens[:i]))
			}
			cur = located{container: v, set: func(root, r any) any { c[tok] = r; return root }}
		case []any:
			idx, err := arrayIndex(tok, len(c), false)
			if err != nil {
				return located{}, fmt.Errorf("at %s: %w", pointerOf(tokens[:i]), err)
			}
			cur = located{container: c[idx], set: func(root, r any) any { c[idx] = r; return root }}
		default:
			return located{}, fmt.Errorf("%s is neither an object nor an array", pointerOf(tokens[:i]))
		}
	}
	return cur, nil
}

func arrayIndex(tok string, n int, insert bool) (int, error) {
	if insert && tok == "-" {
		return n, nil
	}
	idx, err := strconv.Atoi(tok)
	limit := n - 1
	if insert {
		limit = n
	}
	if err != nil || idx < 0 || idx > limit || (tok != "0" && strings.HasPrefix(tok, "0")) {
		return 0, fmt.Errorf("index %q is outside the array of %d", tok, n)
	}
	return idx, nil
}

// pointerTokens splits an RFC 6901 JSON pointer into its unescaped tokens.
func pointerTokens(p string) ([]string, error) {
	if p == "" {
		return nil, nil
	}
	if !strings.HasPrefix(p, "/") {
		return nil, fmt.Errorf("the path %q is not a JSON pointer; it starts with /", p)
	}
	parts := strings.Split(p[1:], "/")
	for i, t := range parts {
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(t, "~1", "/"), "~0", "~")
	}
	return parts, nil
}

func pointerOf(tokens []string) string {
	if len(tokens) == 0 {
		return "the artifact"
	}
	var b strings.Builder
	for _, t := range tokens {
		b.WriteString("/" + strings.ReplaceAll(strings.ReplaceAll(t, "~", "~0"), "/", "~1"))
	}
	return b.String()
}
