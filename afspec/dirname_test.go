package afspec

import "testing"

func TestIsSpecDirName(t *testing.T) {
	for name, want := range map[string]bool{
		"01_feature":            true,
		"99_a":                  true,
		"123_long_feature_name": true,
		"01_a1_b2":              true,
		"1_feature":             false,
		"01feature":             false,
		"01_Feature":            false,
		"01_":                   false,
		"_feature":              false,
		"abc_feature":           false,
		"01-feature":            false,
	} {
		t.Run(name, func(t *testing.T) {
			if got := IsSpecDirName(name); got != want {
				t.Errorf("IsSpecDirName(%q) = %v; want %v", name, got, want)
			}
		})
	}
}

// A spec name is valid exactly when NN_<name> is a valid directory name: no
// doubled or trailing underscore.
func TestIsSpecName_IsTheDirectoryRuleWithoutThePrefix(t *testing.T) {
	for name, want := range map[string]bool{
		"widget_cache": true, "a1": true, "issuex": true, "v2_api": true,
		"widget__cache": false, "issuex_": false, "_x": false, "1x": false, "Widget": false, "": false,
	} {
		if got := IsSpecName(name); got != want {
			t.Errorf("IsSpecName(%q) = %v, want %v", name, got, want)
		}
		if got := IsSpecDirName("07_" + name); got != want {
			t.Errorf("IsSpecDirName(07_%s) = %v, but IsSpecName says %v", name, got, want)
		}
	}
}

func TestParseSpecDirName_SplitsThePrefixFromTheName(t *testing.T) {
	prefix, name, err := ParseSpecDirName("07_agent_mode")
	if err != nil {
		t.Fatalf("ParseSpecDirName = %v", err)
	}
	if prefix != "07" || name != "agent_mode" {
		t.Errorf("= %q/%q; want 07/agent_mode", prefix, name)
	}
	if _, _, err := ParseSpecDirName("not-a-spec"); err == nil {
		t.Error("a malformed name was accepted")
	}
}
