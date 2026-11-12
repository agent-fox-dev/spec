// Package gate runs a spec package's gate: its linter command, then its
// all_tests command, each started directly with no shell, timed, with its
// output kept out of the caller's stdout. It is speca's, and imports nothing
// of the command shell.
package gate

import (
	"errors"
	"strings"
	"unicode"
)

// Split splits a command string into a program and its arguments, on white
// space, a stretch inside single or double quotes kept as one argument without
// its quotes. Nothing else is special: a pipe, &&, a redirection, $VAR or a
// glob is a literal argument, because the program is started without a shell
// (test_commands must need no shell features). An empty string and an
// unterminated quote are errors.
func Split(command string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inArg := false
	var quote rune
	for _, r := range command {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inArg = r, true
		case unicode.IsSpace(r):
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, errors.New("a " + quoteName(quote) + " quote is left open")
	}
	if inArg {
		args = append(args, cur.String())
	}
	if len(args) == 0 {
		return nil, errors.New("the command string is empty")
	}
	return args, nil
}

func quoteName(q rune) string {
	if q == '"' {
		return "double"
	}
	return "single"
}
