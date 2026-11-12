package afspec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InboundHandoff is a handoff another package of the spec root addresses to
// a package, as a renderer reads it: the sending package's directory and id,
// the handoff, and the sentences of the sender's criteria that depend on it.
// Unreadable says why the sending package could not be read, when it could
// not; the handoff then holds its id alone.
type InboundHandoff struct {
	SenderDir  string
	SenderID   string
	Handoff    Handoff
	Criteria   []string
	Unreadable string
}

// Ref is the handoff as a task of the receiver names it in takes_handoffs.
func (in InboundHandoff) Ref() string { return in.SenderID + ":" + in.Handoff.Id }

// InboundHandoffs reads, from the packages under specsDir, every handoff
// addressed to s, in the order of the packages and their handoffs. A package
// a task of s takes a handoff from and that cannot be read is an entry of its
// own, with why, so that a render can say so and go on.
func InboundHandoffs(specsDir string, s *Spec) []InboundHandoff {
	names, err := ListSpecDirs(specsDir)
	if err != nil || s == nil {
		return nil
	}
	taken := map[string][]string{} // sender id -> handoff ids taken from it
	for _, task := range tasksOf(s) {
		for _, ref := range task.TakesHandoffs {
			from, id, _ := strings.Cut(ref, ":")
			taken[from] = append(taken[from], id)
		}
	}
	var out []InboundHandoff
	for _, name := range names {
		dir := filepath.Join(specsDir, name)
		id, _, _ := strings.Cut(name, "_")
		if s.Dir != "" && sameDir(dir, s.Dir) {
			continue
		}
		sender, err := LoadSpec(dir)
		if err != nil {
			for _, h := range taken[id] {
				out = append(out, InboundHandoff{SenderDir: name, SenderID: id, Handoff: Handoff{Id: h}, Unreadable: err.Error()})
			}
			continue
		}
		for _, h := range handoffsOf(sender) {
			if h.To != s.SpecID {
				continue
			}
			in := InboundHandoff{SenderDir: name, SenderID: sender.SpecID, Handoff: h}
			for _, ref := range h.Criteria {
				in.Criteria = append(in.Criteria, criterionSentence(sender, ref))
			}
			out = append(out, in)
		}
	}
	return out
}

// criterionSentence is a criterion of s as its id and EARS sentence.
func criterionSentence(s *Spec, id string) string {
	for _, r := range requirementsOf(s) {
		for _, c := range r.Criteria {
			if c.Id == id {
				return fmt.Sprintf("%s: %s", id, r.Resolve(c).RenderEARSSentence())
			}
		}
	}
	return id
}

// sameDir reports whether two paths are one directory.
func sameDir(a, b string) bool {
	ia, erra := os.Stat(a)
	ib, errb := os.Stat(b)
	return erra == nil && errb == nil && os.SameFile(ia, ib)
}
