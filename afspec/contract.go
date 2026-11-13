package afspec

import (
	"crypto/sha256"
	"fmt"
	"maps"
)

// ContractFiles are the files a package's contract hash covers, in the order
// a caller names them: what an implementer builds against and may not edit
// once the package is active (ADR 26).
var ContractFiles = []string{RequirementsFileName, TasksFileName}

// ComputeContractHash returns, for each of the ContractFiles, the hex SHA-256
// of the file's canonical encoding (the encoding Save writes) with every
// extension key left out. It hashes what the package holds, not the bytes on
// disk, so that another encoding of the same content and an x- key, which the
// format gives no meaning, leave the hash as it was.
//
// Returns a SaveError when an artifact cannot be encoded.
func (s *Spec) ComputeContractHash() (map[string]string, error) {
	out := make(map[string]string, len(ContractFiles))
	for _, file := range ContractFiles {
		var v any
		switch file {
		case RequirementsFileName:
			v = s.Requirements
		case TasksFileName:
			v = s.Tasks
		}
		data, err := marshalArtifact(v, nil)
		if err != nil {
			return nil, saveFailure("cannot encode %s to hash it: %s", file, err)
		}
		out[file] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	return out, nil
}

// ContractDrift names, in the order of ContractFiles, each contract file that
// no longer hashes to the package's ContractHash, a file the hash has no
// entry for among them. It is nil for a package without a contract hash, and
// for one whose files match it.
func (s *Spec) ContractDrift() []string {
	if s == nil || s.ContractHash == nil {
		return nil
	}
	current, err := s.ComputeContractHash()
	if err != nil {
		// A contract that cannot be encoded is not the one that was hashed.
		return append([]string(nil), ContractFiles...)
	}
	var out []string
	for _, file := range ContractFiles {
		if recorded, ok := s.ContractHash[file]; !ok || recorded != current[file] {
			out = append(out, file)
		}
	}
	return out
}

// RequiresEvidence reports whether the package's tasks close on evidence: it
// is active and was activated with a contract hash. A draft, and a package
// activated before the hash was recorded, do not (ADR 26).
func (s *Spec) RequiresEvidence() bool {
	return s != nil && s.Status == string(PrdFrontmatterV3JsonStatusActive) && s.ContractHash != nil
}

// cloneContractHash is a copy of h that shares nothing with it.
func cloneContractHash(h map[string]string) map[string]string {
	if h == nil {
		return nil
	}
	return maps.Clone(h)
}

// TaskEvidence is a copy of the evidence progress.json holds for the task
// with the given id, sharing nothing with the package: for each criterion the
// task owns (for the integration task, each execution path), the tests that
// cover it and the runs speca recorded of them. It is nil for a task with
// none.
func (s *Spec) TaskEvidence(id int) []EvidenceEntry {
	if s == nil || s.Progress == nil {
		return nil
	}
	for _, e := range s.Progress.Tasks {
		if e.Id == id {
			return cloneEvidence(e.Evidence)
		}
	}
	return nil
}

// cloneEvidence is a copy of entries that shares nothing with them, nil for
// none.
func cloneEvidence(entries []EvidenceEntry) []EvidenceEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]EvidenceEntry, len(entries))
	for i, e := range entries {
		out[i] = EvidenceEntry{Covers: e.Covers, Tests: make([]EvidenceTest, len(e.Tests))}
		if e.NoRed != nil {
			reason := *e.NoRed
			out[i].NoRed = &reason
		}
		for j, t := range e.Tests {
			out[i].Tests[j] = EvidenceTest{Test: t.Test, Failed: cloneRun(t.Failed), Passed: cloneRun(t.Passed)}
		}
	}
	return out
}

func cloneRun(r *EvidenceRun) *EvidenceRun {
	if r == nil {
		return nil
	}
	c := EvidenceRun{At: r.At}
	if r.Tree != nil {
		tree := *r.Tree
		c.Tree = &tree
	}
	return &c
}
