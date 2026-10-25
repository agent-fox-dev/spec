package main

import (
	"slices"
	"testing"
)

// --task is a comma-separated list of positive ids, next or all, given once or
// repeated; duplicate ids collapse into one, in the order they first appear.
func TestParseTaskSpec(t *testing.T) {
	for name, tc := range map[string]struct {
		values  []string
		ids     []int
		keyword string
	}{
		"one id":                  {[]string{"3"}, []int{3}, ""},
		"a comma list":            {[]string{"1,2"}, []int{1, 2}, ""},
		"repeated, with a repeat": {[]string{"1,2", "2"}, []int{1, 2}, ""},
		"order of first sight":    {[]string{"3", "1,3,2"}, []int{3, 1, 2}, ""},
		"spaces":                  {[]string{" 1 , 2 "}, []int{1, 2}, ""},
		"next":                    {[]string{"next"}, nil, "next"},
		"all":                     {[]string{"all"}, nil, "all"},
		"next twice":              {[]string{"next", "next"}, nil, "next"},
	} {
		got, err := parseTaskSpec(tc.values)
		if err != nil || !slices.Equal(got.ids, tc.ids) || got.keyword != tc.keyword {
			t.Errorf("%s: parseTaskSpec(%q) = %+v, %v; want ids %v keyword %q", name, tc.values, got, err, tc.ids, tc.keyword)
		}
	}
}

func TestParseTaskSpec_RefusesWhatIsNotAnIDNextOrAll(t *testing.T) {
	for name, values := range map[string][]string{
		"zero": {"0"}, "negative": {"-1"}, "a word": {"abc"}, "an empty item": {"1,,2"}, "an empty value": {""},
		"a trailing comma": {"1,"}, "next with an id": {"next,1"}, "all with an id": {"all,2"}, "next with all": {"next,all"},
		"next and an id, repeated": {"next", "1"}, "a fraction": {"1.5"},
	} {
		if got, err := parseTaskSpec(values); err == nil {
			t.Errorf("%s: parseTaskSpec(%q) = %+v; want an error", name, values, got)
		}
	}
}
