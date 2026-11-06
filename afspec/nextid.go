package afspec

import (
	"fmt"
	"regexp"
	"strconv"
)

// Format v3 has three ID formats (Appendix A), so there are four ID helpers:
// one per format, and one for the criteria inside a requirement. Malformed
// IDs that do not match are silently skipped when scanning for the highest
// suffix in use.
var (
	// nextReqIDRe matches "REQ-3" and captures "3".
	nextReqIDRe = regexp.MustCompile(`^REQ-(\d+)$`)

	// nextCriterionIDRe matches "REQ-2.3" and captures "3".
	nextCriterionIDRe = regexp.MustCompile(`\.(\d+)$`)

	// nextPathIDRe matches "PATH-1" and captures "1".
	nextPathIDRe = regexp.MustCompile(`^PATH-(\d+)$`)
)

// extractMaxSuffix scans a list of IDs, applies the given compiled regex to
// each, extracts the captured numeric suffix from the first submatch group,
// and returns the maximum value found. Returns 0 when nothing matches.
func extractMaxSuffix(ids []string, re *regexp.Regexp) int {
	max := 0
	for _, id := range ids {
		m := re.FindStringSubmatch(id)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return max
}

// NextRequirementID returns the next free requirement ID, REQ-{N}.
func NextRequirementID(req RequirementsV3Json) string {
	ids := make([]string, len(req.Requirements))
	for i, r := range req.Requirements {
		ids[i] = r.Id
	}
	return fmt.Sprintf("REQ-%d", extractMaxSuffix(ids, nextReqIDRe)+1)
}

// NextCriterionID returns the next free criterion ID within a requirement,
// {requirement_id}.{C}.
func NextCriterionID(r Requirement) string {
	ids := make([]string, len(r.Criteria))
	for i, c := range r.Criteria {
		ids[i] = c.Id
	}
	return fmt.Sprintf("%s.%d", r.Id, extractMaxSuffix(ids, nextCriterionIDRe)+1)
}

// NextExecutionPathID returns the next free path ID, PATH-{N}.
func NextExecutionPathID(req RequirementsV3Json) string {
	ids := make([]string, len(req.ExecutionPaths))
	for i, ep := range req.ExecutionPaths {
		ids[i] = ep.Id
	}
	return fmt.Sprintf("PATH-%d", extractMaxSuffix(ids, nextPathIDRe)+1)
}

// NextTaskID returns the next free task ID. Task IDs are plain integers
// sequential from 1 (Appendix A); on a full package the next free id is the
// integration task's.
func NextTaskID(t TasksV3Json) int {
	max := 0
	for _, task := range t.Tasks {
		if task.Id > max {
			max = task.Id
		}
	}
	return max + 1
}
