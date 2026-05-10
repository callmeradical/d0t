// Package selfupdate implements d0t update: rebuild the d0t binary.
package selfupdate_test

import (
	"testing"

	"d0t/internal/selfupdate"
)

// ---------------------------------------------------------------------------
// SelfUpdate: rebuild from source
// ---------------------------------------------------------------------------

func TestSelfUpdate_BuildsFromSource(t *testing.T) {
	built := false
	u := selfupdate.New(selfupdate.Config{
		SourcePath: "/some/source",
		OutputPath: "/usr/local/bin/d0t",
		Build: func(src, out string) error {
			if src != "/some/source" {
				t.Errorf("src = %q, want /some/source", src)
			}
			if out != "/usr/local/bin/d0t" {
				t.Errorf("out = %q, want /usr/local/bin/d0t", out)
			}
			built = true
			return nil
		},
	})
	if err := u.Run(); err != nil {
		t.Fatal(err)
	}
	if !built {
		t.Error("expected build to be called")
	}
}

func TestSelfUpdate_NoSourceReturnsError(t *testing.T) {
	u := selfupdate.New(selfupdate.Config{
		SourcePath: "",
		OutputPath: "/usr/local/bin/d0t",
		Build:      func(_, _ string) error { return nil },
	})
	if err := u.Run(); err == nil {
		t.Error("expected error when no source path configured")
	}
}

func TestSelfUpdate_NoOutputPathReturnsError(t *testing.T) {
	u := selfupdate.New(selfupdate.Config{
		SourcePath: "/some/source",
		OutputPath: "",
		Build:      func(_, _ string) error { return nil },
	})
	if err := u.Run(); err == nil {
		t.Error("expected error when no output path configured")
	}
}
