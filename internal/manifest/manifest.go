// Package manifest parses d0tfile, the explicit resource manifest for a d0t
// profile. Each line is one declaration:
//
//	link   home/.zshrc
//	copy   home/.ssh/config   mode=0600
//	tmpl   home/.gitconfig
//	fragment fragments/path.fragment
//	hook   post-apply hooks/post-apply.sh  optional
//	brew   ripgrep neovim bat
//	cask   ghostty raycast
//	tap    homebrew/cask-fonts
//	mas    497799835 Xcode
//	apt    ripgrep fd-find
//
// Lines beginning with # and blank lines are ignored. Inline comments are
// supported (everything after unquoted # is stripped).
package manifest

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

// Declaration is implemented by FileDecl, HookDecl, and PkgDecl.
type Declaration interface {
	declMarker()
}

// FileDecl represents a link, copy, template, or fragment resource.
type FileDecl struct {
	// Kind is "link", "copy", "template", or "fragment".
	Kind string
	// Source is the path relative to the profile directory.
	Source string
	// Target is an explicit target path override. Empty means infer from
	// the Source's root prefix (home/ → $HOME, xdg/ → ~/.config, etc.).
	Target string
	// Mode is the file permission for copy resources. Zero means preserve
	// the source file's mode.
	Mode fs.FileMode
	// Marker is the fragment block identifier. Empty means read from
	// the fragment file's frontmatter.
	Marker string
}

func (*FileDecl) declMarker() {}

// HookDecl represents an exec hook at a lifecycle phase.
type HookDecl struct {
	// Phase is one of "pre-apply", "post-apply", "pre-remove", "post-remove".
	Phase string
	// Script is the path to the hook script, relative to the profile dir.
	Script string
	// Optional, when true, means a non-zero exit is logged but not fatal.
	Optional bool
}

func (*HookDecl) declMarker() {}

// PkgDecl represents a package manager directive.
type PkgDecl struct {
	// Manager is "brew", "cask", "tap", "mas", or "apt".
	Manager string
	// Items holds package names for brew/cask/tap/apt.
	Items []string
	// MasApps holds Mac App Store app declarations.
	MasApps []MasApp
}

func (*PkgDecl) declMarker() {}

// MasApp is a single Mac App Store app declaration.
type MasApp struct {
	ID   int
	Name string
}

// validPhases is the set of recognized hook phase names.
var validPhases = map[string]bool{
	"pre-apply":  true,
	"post-apply": true,
	"pre-remove": true,
	"post-remove": true,
}

// Parse reads a d0tfile and returns the ordered list of declarations.
func Parse(r io.Reader) ([]Declaration, error) {
	var decls []Declaration
	sc := bufio.NewScanner(r)
	lineNum := 0

	for sc.Scan() {
		lineNum++
		line := stripComment(sc.Text())
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		d, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}
		decls = append(decls, d)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return decls, nil
}

// Load reads a d0tfile from a profile directory. Returns (nil, nil) if
// the file does not exist — callers fall back to convention-based discovery.
func Load(profilePath string) ([]Declaration, error) {
	path := profilePath + "/d0tfile"
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open d0tfile: %w", err)
	}
	defer f.Close()
	decls, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return decls, nil
}

// ---------------------------------------------------------------------------
// internal
// ---------------------------------------------------------------------------

func parseLine(line string) (Declaration, error) {
	tokens := tokenize(line)
	if len(tokens) == 0 {
		return nil, nil
	}
	keyword := strings.ToLower(tokens[0])
	rest := tokens[1:]

	switch keyword {
	case "link", "copy", "tmpl", "template", "fragment":
		return parseFileDecl(keyword, rest)
	case "hook":
		return parseHookDecl(rest)
	case "brew", "cask", "tap", "apt":
		return parsePkgDecl(keyword, rest)
	case "mas":
		return parseMasDecl(rest)
	default:
		return nil, fmt.Errorf("unknown keyword %q", keyword)
	}
}

func parseFileDecl(keyword string, args []string) (*FileDecl, error) {
	kind := keyword
	if kind == "tmpl" {
		kind = "template"
	}

	positional, opts := splitOpts(args)
	if len(positional) == 0 {
		return nil, fmt.Errorf("%s: source path required", kind)
	}

	d := &FileDecl{
		Kind:   kind,
		Source: positional[0],
		Target: opts["target"],
		Marker: opts["marker"],
	}

	if modeStr, ok := opts["mode"]; ok {
		m, err := strconv.ParseUint(modeStr, 8, 32)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid mode %q (expected octal like 0600)", kind, modeStr)
		}
		d.Mode = fs.FileMode(m)
	}
	return d, nil
}

func parseHookDecl(args []string) (*HookDecl, error) {
	// hook <phase> <script> [optional]
	positional, _ := splitOpts(args)
	if len(positional) < 2 {
		return nil, fmt.Errorf("hook: requires <phase> <script>")
	}
	phase := strings.ToLower(positional[0])
	if !validPhases[phase] {
		return nil, fmt.Errorf("hook: unknown phase %q (want pre-apply, post-apply, pre-remove, post-remove)", phase)
	}
	optional := false
	for _, a := range positional[2:] {
		if strings.ToLower(a) == "optional" {
			optional = true
		}
	}
	return &HookDecl{
		Phase:    phase,
		Script:   positional[1],
		Optional: optional,
	}, nil
}

func parsePkgDecl(manager string, args []string) (*PkgDecl, error) {
	positional, _ := splitOpts(args)
	if len(positional) == 0 {
		return nil, fmt.Errorf("%s: at least one package required", manager)
	}
	return &PkgDecl{Manager: manager, Items: positional}, nil
}

func parseMasDecl(args []string) (*PkgDecl, error) {
	// mas <id> <name...>
	positional, _ := splitOpts(args)
	if len(positional) < 2 {
		return nil, fmt.Errorf("mas: requires <id> <name>")
	}
	id, err := strconv.Atoi(positional[0])
	if err != nil {
		return nil, fmt.Errorf("mas: ID must be a number, got %q", positional[0])
	}
	name := strings.Join(positional[1:], " ")
	return &PkgDecl{
		Manager: "mas",
		MasApps: []MasApp{{ID: id, Name: name}},
	}, nil
}

// splitOpts separates positional args from key=value options.
func splitOpts(tokens []string) (positional []string, opts map[string]string) {
	opts = map[string]string{}
	for _, t := range tokens {
		if k, v, ok := strings.Cut(t, "="); ok {
			opts[k] = v
		} else {
			positional = append(positional, t)
		}
	}
	return
}

// tokenize splits a line on whitespace.
func tokenize(line string) []string {
	return strings.Fields(line)
}

// stripComment removes everything from the first unquoted # onward.
func stripComment(line string) string {
	if i := strings.Index(line, "#"); i >= 0 {
		return line[:i]
	}
	return line
}
