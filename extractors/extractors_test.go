package extractors

import (
	"testing"
)

func TestParseSources(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"all", len(Registry)},
		{"", len(Registry)},
		{"safari_history", 1},
		{"safari_history,calendar", 2},
		{"safari_history, calendar, notes", 3},
	}

	for _, tt := range tests {
		got := ParseSources(tt.input)
		if len(got) != tt.want {
			t.Errorf("ParseSources(%q) returned %d sources, want %d", tt.input, len(got), tt.want)
		}
	}
}

func TestRegistryHasAllSources(t *testing.T) {
	expected := []string{"safari_history", "safari_bookmarks", "calendar", "reminders", "notes", "zotero"}
	for _, src := range expected {
		if _, ok := Registry[src]; !ok {
			t.Errorf("Registry missing expected source: %s", src)
		}
	}
}

func TestAllSourcesMatchesRegistry(t *testing.T) {
	all := AllSources()
	if len(all) != len(Registry) {
		t.Errorf("AllSources() returned %d, Registry has %d", len(all), len(Registry))
	}
	seen := make(map[string]bool)
	for _, s := range all {
		if seen[s] {
			t.Errorf("duplicate source: %s", s)
		}
		seen[s] = true
		if _, ok := Registry[s]; !ok {
			t.Errorf("AllSources() returned %q which is not in Registry", s)
		}
	}
}
