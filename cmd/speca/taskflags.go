package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/agent-fox-dev/spec/afspec"
)

// taskStates are the four states of a task, in the order of its life: the
// values --state takes.
var taskStates = []string{
	string(afspec.TaskStatePending),
	string(afspec.TaskStateInProgress),
	string(afspec.TaskStateDone),
	string(afspec.TaskStateDropped),
}

// taskFlag collects the values of --task, given once or repeated. Each value is
// checked with the rest of the call, so that one message can name every fault.
type taskFlag []string

func (f *taskFlag) String() string { return strings.Join(*f, ",") }

func (f *taskFlag) Set(v string) error {
	*f = append(*f, v)
	return nil
}

// taskSpec is what --task names: task ids, or one of the keywords next and all
// (and then no ids).
type taskSpec struct {
	// ids are the ids named, each once, in the order they first appear.
	ids []int
	// keyword is "next", "all" or "".
	keyword string
}

// parseTaskSpec reads the values of --task. Each is a comma-separated list
// whose items are a task id, a whole number of 1 or more, or the keywords next
// and all; next and all must each be the only item of the whole flag. It
// returns an error naming every fault it finds.
func parseTaskSpec(values []string) (taskSpec, error) {
	var spec taskSpec
	var errs []error
	var keywords []string
	for _, v := range values {
		for _, item := range strings.Split(v, ",") {
			item = strings.TrimSpace(item)
			switch {
			case item == "":
				errs = append(errs, fmt.Errorf("--task has an empty item in %q: give task ids, next or all", v))
			case item == "next" || item == "all":
				if !slices.Contains(keywords, item) {
					keywords = append(keywords, item)
				}
			default:
				id, err := strconv.Atoi(item)
				if err != nil || id < 1 {
					errs = append(errs, fmt.Errorf("--task value %q is not a task id (a whole number, 1 or more), next or all", item))
				} else if !slices.Contains(spec.ids, id) {
					spec.ids = append(spec.ids, id)
				}
			}
		}
	}
	given := strings.Join(values, ",")
	switch {
	case len(keywords) > 1:
		errs = append(errs, fmt.Errorf("--task next and all each stand alone: give one of them, or ids (got %q)", given))
	case len(keywords) == 1 && len(spec.ids) > 0:
		errs = append(errs, fmt.Errorf("--task %s stands alone: it cannot be given with task ids (got %q)", keywords[0], given))
	case len(keywords) == 1:
		spec.keyword = keywords[0]
	}
	return spec, errors.Join(errs...)
}
