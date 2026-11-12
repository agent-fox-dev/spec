package gate

import (
	"slices"
	"testing"
)

// A command string is split on white space, a quoted stretch kept as one
// argument without its quotes, and nothing a shell would read is special.
func TestSplit_KeepsQuotedStretchesAndNothingElse(t *testing.T) {
	for in, want := range map[string][]string{
		"make test":                  {"make", "test"},
		"go test ./... -run 'A|B'":   {"go", "test", "./...", "-run", "A|B"},
		`x "a b" c`:                  {"x", "a b", "c"},
		"x  a\tb":                    {"x", "a", "b"},
		"x && y > z $HOME *":         {"x", "&&", "y", ">", "z", "$HOME", "*"},
		`x 'say "hi"' "it's"`:        {"x", `say "hi"`, "it's"},
		"  leading and trailing  ":   {"leading", "and", "trailing"},
		`x a"b c"d`:                  {"x", "ab cd"},
		"x ''":                       {"x", ""},
		"go vet ./... | tee out.txt": {"go", "vet", "./...", "|", "tee", "out.txt"},
	} {
		got, err := Split(in)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("Split(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestSplit_RefusesAnEmptyStringAndAnOpenQuote(t *testing.T) {
	for _, in := range []string{"", "   ", "\t\n", "x 'a", `x "a`, `make 'lint`} {
		if got, err := Split(in); err == nil {
			t.Errorf("Split(%q) = %q; want an error", in, got)
		}
	}
}
