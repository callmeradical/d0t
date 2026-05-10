package primitive_test

import (
	"testing"

	"github.com/callmeradical/d0t/internal/primitive"
)

// MergeManifests is used internally by PkgActionsFromDeclarations to
// deduplicate packages accumulated across profiles.

func TestMergeManifests_DeduplicatesAndUnions(t *testing.T) {
	a := &primitive.PkgManifest{
		Brew: primitive.BrewManifest{
			Formulae: []string{"ripgrep", "fd"},
			Casks:    []string{"ghostty"},
		},
	}
	b := &primitive.PkgManifest{
		Brew: primitive.BrewManifest{
			Formulae: []string{"fd", "bat"}, // "fd" is a duplicate
		},
	}
	merged := primitive.MergeManifests([]*primitive.PkgManifest{a, b})
	if len(merged.Brew.Formulae) != 3 {
		t.Errorf("formulae count = %d, want 3 (ripgrep, fd, bat)", len(merged.Brew.Formulae))
	}
	if len(merged.Brew.Casks) != 1 {
		t.Errorf("casks count = %d, want 1", len(merged.Brew.Casks))
	}
}

func TestMergeManifests_NilInput(t *testing.T) {
	merged := primitive.MergeManifests(nil)
	if merged == nil {
		t.Error("MergeManifests(nil) should not return nil")
	}
}
