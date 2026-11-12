package gate

import (
	"bytes"
	"os"
	"strings"
)

// TailLines is how many of a failing command's last lines the verdict holds.
const TailLines = 40

// sink is where a command's stdout and stderr go together: its log file, when
// there is one, the tail, and the units reader, when its units are read. It
// never fails a write, so that a log that cannot be written does not end the
// command.
type sink struct {
	log   *os.File
	tail  tail
	units *units
}

func (s *sink) Write(p []byte) (int, error) {
	if s.log != nil {
		if _, err := s.log.Write(p); err != nil {
			s.log = nil
		}
	}
	s.tail.Write(p)
	if s.units != nil {
		s.units.Write(p)
	}
	return len(p), nil
}

// tail keeps the last TailLines lines of an output as it is written, without
// the output ever being read back: a ring of whole lines and the line being
// written.
type tail struct {
	lines   []string
	next    int // where the next whole line goes once the ring is full
	partial []byte
}

func (t *tail) Write(p []byte) {
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			t.partial = append(t.partial, p...)
			return
		}
		t.push(string(append(t.partial, p[:i]...)))
		t.partial = t.partial[:0]
		p = p[i+1:]
	}
}

func (t *tail) push(line string) {
	if len(t.lines) < TailLines {
		t.lines = append(t.lines, line)
		return
	}
	t.lines[t.next] = line
	t.next = (t.next + 1) % TailLines
}

// String is the last TailLines lines in order, joined by newlines; a last line
// without a newline counts as a line. It is empty when nothing was written.
func (t *tail) String() string {
	ordered := append(append([]string{}, t.lines[t.next:]...), t.lines[:t.next]...)
	if len(t.partial) > 0 {
		ordered = append(ordered, string(t.partial))
		if len(ordered) > TailLines {
			ordered = ordered[len(ordered)-TailLines:]
		}
	}
	return strings.Join(ordered, "\n")
}
