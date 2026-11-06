package main

import (
	"os"
	"path/filepath"
)

// dirMode and fileMode are what the render writes with. They are fixed so that
// two runs over one source tree write the same tree, down to the modes.
const (
	dirMode  = 0o755
	fileMode = 0o644
)

// write renders every skill for every agent into out. It replaces whatever was
// there, so a file or a skill removed from the source is not left behind, and
// it walks the agents and the skills in a fixed order, taking nothing from the
// clock, the environment or map iteration, so a rerun writes the same bytes.
//
// It runs only once every check has passed: a run that found a problem leaves
// the previous output exactly as it was.
func (s *source) write(out string) error {
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	for _, a := range agents {
		for _, sk := range s.skills {
			if err := writeSkill(filepath.Join(out, a.name, sk.name), renderSkill(sk, a)); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeSkill writes one rendered skill: its SKILL.md and a references/ holding
// exactly the files that skill includes. The directory is written even when the
// skill includes no file, so every installed skill has the same shape.
func writeSkill(dir string, r rendered) error {
	refs := filepath.Join(dir, refsSubdir)
	if err := os.MkdirAll(refs, dirMode); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, skillDoc), []byte(r.doc), fileMode); err != nil {
		return err
	}
	for _, name := range sortedKeys(r.refs) {
		if err := os.WriteFile(filepath.Join(refs, name), []byte(r.refs[name]), fileMode); err != nil {
			return err
		}
	}
	return nil
}
