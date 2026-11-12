package afspec

// renderConfig holds the resolved rendering options.
type renderConfig struct {
	maxTokens int
	inbound   []InboundHandoff
}

// WithInboundHandoffs gives a scoped render the handoffs other packages of
// the spec root address to the package, as InboundHandoffs reads them: a task
// that takes one is rendered with the sender's text, and the integration task
// with a step and a contract for each. A package's own render never reads
// another package, so that its spec.md depends on its own files alone.
func WithInboundHandoffs(in []InboundHandoff) RenderOption {
	return func(c *renderConfig) {
		c.inbound = in
	}
}

// RenderOption configures a render call.
type RenderOption func(*renderConfig)

// WithMaxTokens caps the estimated token count of the rendered output.
// Values below 1 disable the budget.
func WithMaxTokens(n int) RenderOption {
	return func(c *renderConfig) {
		c.maxTokens = n
	}
}

func resolveOpts(opts []RenderOption) renderConfig {
	var cfg renderConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

func budgetActive(cfg renderConfig) bool { return cfg.maxTokens > 0 }

func sumMapTokens(m map[string]string) int {
	total := 0
	for _, v := range m {
		total += EstimateTokens(v)
	}
	return total
}
