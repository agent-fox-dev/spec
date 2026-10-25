package afspec

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestDiscoverSpecs_ListsThePackagesInPrefixOrder(t *testing.T) {
	root := specRoot(t, map[string]string{
		"01_test_feature":  fixtureValidSpec,
		"02_draft_feature": fixtureDraft,
		"not_a_spec_dir":   fixtureValidSpec,
	})

	metas, err := DiscoverSpecs(root)
	if err != nil {
		t.Fatalf("DiscoverSpecs = %v", err)
	}
	checkTwoSpecs(t, metas)
}

// A package whose prd.md does not parse is skipped and reported, and the
// others are still listed.
func TestDiscoverSpecs_SkipsAndReportsAnUnreadablePackage(t *testing.T) {
	root := specRoot(t, map[string]string{
		"01_test_feature":  fixtureValidSpec,
		"02_draft_feature": fixtureDraft,
		"03_broken":        "testdata/malformed_yaml",
	})
	metas, err := DiscoverSpecs(root)
	if err == nil {
		t.Error("DiscoverSpecs = nil error; want the broken package reported")
	}
	checkTwoSpecs(t, metas)

	if _, err := DiscoverSpecs(filepath.Join(root, "missing")); err == nil {
		t.Error("DiscoverSpecs on a missing root = nil error")
	}
}

func TestLoadSpecLandscape_AddsTheArchiveAndLeavesOutTheCurrentSpec(t *testing.T) {
	root := specRoot(t, map[string]string{
		"01_test_feature":          fixtureValidSpec,
		"02_draft_feature":         fixtureDraft,
		"archive/03_alpha_feature": "testdata/alpha_prefix_spec",
	})
	ids := func(metas []SpecMeta) []string {
		var out []string
		for _, m := range metas {
			out = append(out, m.SpecID)
		}
		slices.Sort(out)
		return out
	}
	for _, tc := range []struct {
		name           string
		includeArchive bool
		current        string
		want           []string
	}{
		{"without the archive", false, "", []string{"01", "02"}},
		{"with the archive", true, "", []string{"01", "02", "abc"}},
		{"without the current spec", true, "02", []string{"01", "abc"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metas, err := LoadSpecLandscape(root, tc.includeArchive, tc.current)
			if err != nil {
				t.Fatalf("LoadSpecLandscape = %v", err)
			}
			if got := ids(metas); !slices.Equal(got, tc.want) {
				t.Errorf("specs = %v; want %v", got, tc.want)
			}
		})
	}

	metas, err := LoadSpecLandscape(filepath.Join(root, "missing"), true, "")
	if err != nil || metas == nil || len(metas) != 0 {
		t.Errorf("LoadSpecLandscape on a missing root = %v, %v; want an empty, non-nil slice", metas, err)
	}
}
