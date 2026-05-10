package manifest_test

import (
	"strings"
	"testing"

	"d0t/internal/manifest"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func parse(t *testing.T, src string) []manifest.Declaration {
	t.Helper()
	decls, err := manifest.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	return decls
}

func parseErr(t *testing.T, src string) error {
	t.Helper()
	_, err := manifest.Parse(strings.NewReader(src))
	return err
}

func fileDecl(t *testing.T, decls []manifest.Declaration, n int) *manifest.FileDecl {
	t.Helper()
	if n >= len(decls) {
		t.Fatalf("want decl[%d], only have %d declarations", n, len(decls))
	}
	fd, ok := decls[n].(*manifest.FileDecl)
	if !ok {
		t.Fatalf("decl[%d] is %T, want *FileDecl", n, decls[n])
	}
	return fd
}

func hookDecl(t *testing.T, decls []manifest.Declaration, n int) *manifest.HookDecl {
	t.Helper()
	if n >= len(decls) {
		t.Fatalf("want decl[%d], only have %d declarations", n, len(decls))
	}
	hd, ok := decls[n].(*manifest.HookDecl)
	if !ok {
		t.Fatalf("decl[%d] is %T, want *HookDecl", n, decls[n])
	}
	return hd
}

func pkgDecl(t *testing.T, decls []manifest.Declaration, n int) *manifest.PkgDecl {
	t.Helper()
	if n >= len(decls) {
		t.Fatalf("want decl[%d], only have %d declarations", n, len(decls))
	}
	pd, ok := decls[n].(*manifest.PkgDecl)
	if !ok {
		t.Fatalf("decl[%d] is %T, want *PkgDecl", n, decls[n])
	}
	return pd
}

// ---------------------------------------------------------------------------
// blank lines and comments
// ---------------------------------------------------------------------------

func TestParse_EmptyReturnsNil(t *testing.T) {
	decls := parse(t, "")
	if len(decls) != 0 {
		t.Errorf("expected 0 declarations, got %d", len(decls))
	}
}

func TestParse_SkipsBlankLinesAndComments(t *testing.T) {
	src := `
# this is a comment

  # indented comment

link home/.zshrc
# another comment
`
	decls := parse(t, src)
	if len(decls) != 1 {
		t.Fatalf("expected 1 declaration, got %d", len(decls))
	}
}

func TestParse_InlineComment(t *testing.T) {
	decls := parse(t, "link home/.zshrc  # my shell config")
	fd := fileDecl(t, decls, 0)
	if fd.Source != "home/.zshrc" {
		t.Errorf("source = %q, want %q", fd.Source, "home/.zshrc")
	}
}

// ---------------------------------------------------------------------------
// link
// ---------------------------------------------------------------------------

func TestParse_Link(t *testing.T) {
	fd := fileDecl(t, parse(t, "link home/.zshrc"), 0)
	if fd.Kind != "link" {
		t.Errorf("kind = %q, want link", fd.Kind)
	}
	if fd.Source != "home/.zshrc" {
		t.Errorf("source = %q, want home/.zshrc", fd.Source)
	}
	if fd.Target != "" {
		t.Errorf("target should be empty (inferred), got %q", fd.Target)
	}
}

func TestParse_LinkWithExplicitTarget(t *testing.T) {
	fd := fileDecl(t, parse(t, "link home/.zshrc  target=~/.config/zsh/zshrc"), 0)
	if fd.Target != "~/.config/zsh/zshrc" {
		t.Errorf("target = %q, want ~/.config/zsh/zshrc", fd.Target)
	}
}

// ---------------------------------------------------------------------------
// copy
// ---------------------------------------------------------------------------

func TestParse_Copy(t *testing.T) {
	fd := fileDecl(t, parse(t, "copy home/.ssh/config"), 0)
	if fd.Kind != "copy" {
		t.Errorf("kind = %q, want copy", fd.Kind)
	}
	if fd.Source != "home/.ssh/config" {
		t.Errorf("source = %q", fd.Source)
	}
}

func TestParse_CopyWithMode(t *testing.T) {
	fd := fileDecl(t, parse(t, "copy home/.ssh/config  mode=0600"), 0)
	if fd.Mode != 0o600 {
		t.Errorf("mode = %04o, want 0600", fd.Mode)
	}
}

func TestParse_CopyInvalidMode(t *testing.T) {
	if err := parseErr(t, "copy home/.ssh/config  mode=notanumber"); err == nil {
		t.Error("expected error for invalid mode, got nil")
	}
}

// ---------------------------------------------------------------------------
// tmpl / template
// ---------------------------------------------------------------------------

func TestParse_Tmpl(t *testing.T) {
	fd := fileDecl(t, parse(t, "tmpl home/.gitconfig"), 0)
	if fd.Kind != "template" {
		t.Errorf("kind = %q, want template", fd.Kind)
	}
}

func TestParse_TemplateKeyword(t *testing.T) {
	fd := fileDecl(t, parse(t, "template home/.gitconfig"), 0)
	if fd.Kind != "template" {
		t.Errorf("kind = %q, want template", fd.Kind)
	}
}

// ---------------------------------------------------------------------------
// fragment
// ---------------------------------------------------------------------------

func TestParse_Fragment(t *testing.T) {
	fd := fileDecl(t, parse(t, "fragment fragments/path.fragment"), 0)
	if fd.Kind != "fragment" {
		t.Errorf("kind = %q, want fragment", fd.Kind)
	}
	if fd.Source != "fragments/path.fragment" {
		t.Errorf("source = %q", fd.Source)
	}
}

func TestParse_FragmentWithInlineTargetAndMarker(t *testing.T) {
	fd := fileDecl(t, parse(t, "fragment fragments/path.fragment  target=~/.zshrc  marker=local-bin"), 0)
	if fd.Target != "~/.zshrc" {
		t.Errorf("target = %q, want ~/.zshrc", fd.Target)
	}
	if fd.Marker != "local-bin" {
		t.Errorf("marker = %q, want local-bin", fd.Marker)
	}
}

// ---------------------------------------------------------------------------
// hook
// ---------------------------------------------------------------------------

func TestParse_Hook(t *testing.T) {
	hd := hookDecl(t, parse(t, "hook post-apply hooks/post-apply.sh"), 0)
	if hd.Phase != "post-apply" {
		t.Errorf("phase = %q, want post-apply", hd.Phase)
	}
	if hd.Script != "hooks/post-apply.sh" {
		t.Errorf("script = %q, want hooks/post-apply.sh", hd.Script)
	}
	if hd.Optional {
		t.Error("should not be optional")
	}
}

func TestParse_HookOptional(t *testing.T) {
	hd := hookDecl(t, parse(t, "hook post-apply hooks/post-apply.sh  optional"), 0)
	if !hd.Optional {
		t.Error("expected Optional = true")
	}
}

func TestParse_HookInvalidPhase(t *testing.T) {
	if err := parseErr(t, "hook bad-phase hooks/foo.sh"); err == nil {
		t.Error("expected error for invalid phase, got nil")
	}
}

// ---------------------------------------------------------------------------
// pkg — brew
// ---------------------------------------------------------------------------

func TestParse_Brew(t *testing.T) {
	pd := pkgDecl(t, parse(t, "brew ripgrep neovim bat"), 0)
	if pd.Manager != "brew" {
		t.Errorf("manager = %q, want brew", pd.Manager)
	}
	if len(pd.Items) != 3 {
		t.Fatalf("items count = %d, want 3", len(pd.Items))
	}
	if pd.Items[0] != "ripgrep" || pd.Items[1] != "neovim" || pd.Items[2] != "bat" {
		t.Errorf("items = %v", pd.Items)
	}
}

func TestParse_Cask(t *testing.T) {
	pd := pkgDecl(t, parse(t, "cask ghostty raycast"), 0)
	if pd.Manager != "cask" {
		t.Errorf("manager = %q, want cask", pd.Manager)
	}
	if len(pd.Items) != 2 {
		t.Errorf("items count = %d, want 2", len(pd.Items))
	}
}

func TestParse_Tap(t *testing.T) {
	pd := pkgDecl(t, parse(t, "tap homebrew/cask-fonts"), 0)
	if pd.Manager != "tap" {
		t.Errorf("manager = %q, want tap", pd.Manager)
	}
	if len(pd.Items) != 1 || pd.Items[0] != "homebrew/cask-fonts" {
		t.Errorf("items = %v", pd.Items)
	}
}

func TestParse_Apt(t *testing.T) {
	pd := pkgDecl(t, parse(t, "apt ripgrep fd-find"), 0)
	if pd.Manager != "apt" {
		t.Errorf("manager = %q, want apt", pd.Manager)
	}
}

func TestParse_Mas(t *testing.T) {
	pd := pkgDecl(t, parse(t, "mas 497799835 Xcode"), 0)
	if pd.Manager != "mas" {
		t.Errorf("manager = %q, want mas", pd.Manager)
	}
	if len(pd.MasApps) != 1 {
		t.Fatalf("mas apps count = %d, want 1", len(pd.MasApps))
	}
	if pd.MasApps[0].ID != 497799835 {
		t.Errorf("ID = %d, want 497799835", pd.MasApps[0].ID)
	}
	if pd.MasApps[0].Name != "Xcode" {
		t.Errorf("Name = %q, want Xcode", pd.MasApps[0].Name)
	}
}

func TestParse_MasMultiWordName(t *testing.T) {
	pd := pkgDecl(t, parse(t, "mas 497799835 Final Cut Pro"), 0)
	if pd.MasApps[0].Name != "Final Cut Pro" {
		t.Errorf("Name = %q, want %q", pd.MasApps[0].Name, "Final Cut Pro")
	}
}

func TestParse_MasMissingID(t *testing.T) {
	if err := parseErr(t, "mas notanumber Xcode"); err == nil {
		t.Error("expected error for non-numeric mas ID, got nil")
	}
}

// ---------------------------------------------------------------------------
// unknown keyword
// ---------------------------------------------------------------------------

func TestParse_UnknownKeywordErrors(t *testing.T) {
	if err := parseErr(t, "symlink home/.zshrc"); err == nil {
		t.Error("expected error for unknown keyword, got nil")
	}
}

// ---------------------------------------------------------------------------
// full manifest round-trip
// ---------------------------------------------------------------------------

func TestParse_FullManifest(t *testing.T) {
	src := `
# base profile

link home/.zshrc
link home/.zshenv
link xdg/nvim

copy home/.ssh/config  mode=0600

tmpl home/.gitconfig

fragment fragments/path.fragment

hook pre-apply  hooks/pre-apply.sh
hook post-apply hooks/post-apply.sh  optional

brew  ripgrep neovim bat fzf lazygit starship tmux
cask  ghostty raycast
tap   homebrew/cask-fonts
mas   497799835 Xcode
apt   ripgrep neovim
`
	decls := parse(t, src)

	var files, hooks, pkgs int
	for _, d := range decls {
		switch d.(type) {
		case *manifest.FileDecl:
			files++
		case *manifest.HookDecl:
			hooks++
		case *manifest.PkgDecl:
			pkgs++
		}
	}

	if files != 6 { // link×3, copy, tmpl, fragment
		t.Errorf("file decls = %d, want 6", files)
	}
	if hooks != 2 {
		t.Errorf("hook decls = %d, want 2", hooks)
	}
	if pkgs != 5 { // brew, cask, tap, mas, apt
		t.Errorf("pkg decls = %d, want 5", pkgs)
	}
}

// ---------------------------------------------------------------------------
// error reporting includes line number
// ---------------------------------------------------------------------------

func TestParse_ErrorIncludesLineNumber(t *testing.T) {
	src := "link home/.zshrc\ncopy home/.ssh/config mode=bad\n"
	err := parseErr(t, src)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "2") {
		t.Errorf("error should mention line 2, got: %v", err)
	}
}
