package afspec

import (
	"strings"
	"testing"
)

func TestEstimateTokens_GrowsWithTheText(t *testing.T) {
	if got := EstimateTokens(""); got != 0 {
		t.Errorf("EstimateTokens(\"\") = %d; want 0", got)
	}
	short, long := EstimateTokens("a short string"), EstimateTokens(strings.Repeat("a longer string ", 100))
	if long <= short {
		t.Errorf("a longer text estimated %d tokens, a shorter one %d", long, short)
	}
}
