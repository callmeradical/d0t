package primitive

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"d0t/internal/fsutil"
	"d0t/internal/plan"
)

// FragmentHeader is the parsed frontmatter from a .fragment source file.
type FragmentHeader struct {
	// RawTarget is the unexpanded target path from the header (may contain ~ or $VAR).
	RawTarget string
	// Marker is the unique identifier for this block within the target file.
	Marker string
}

// ParseFragmentHeader parses the first line of a .fragment file as a d0t
// frontmatter header. Format:
//
//	# d0t: target=<path> marker=<id>
//
// Returns the header, the body (everything after the first line), and any
// error. Fields can appear in any order.
func ParseFragmentHeader(src string) (FragmentHeader, string, error) {
	line, body, _ := strings.Cut(src, "\n")
	if body != "" {
		body = body + "" // ensure no modification
	}
	line = strings.TrimSpace(line)
	prefix := "# d0t:"
	if !strings.HasPrefix(line, prefix) {
		return FragmentHeader{}, "", fmt.Errorf("fragment: first line must be %q, got %q", prefix, line)
	}
	rest := strings.TrimSpace(line[len(prefix):])
	var hdr FragmentHeader
	for _, part := range strings.Fields(rest) {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch k {
		case "target":
			hdr.RawTarget = v
		case "marker":
			hdr.Marker = v
		}
	}
	if hdr.RawTarget == "" {
		return FragmentHeader{}, "", fmt.Errorf("fragment: header missing target= field")
	}
	if hdr.Marker == "" {
		return FragmentHeader{}, "", fmt.Errorf("fragment: header missing marker= field")
	}
	return hdr, body, nil
}

// Fragment inserts and maintains a managed block inside a target file that
// d0t does not own. The block is delimited by unique marker comments so it
// can be reliably located, updated, and removed.
type Fragment struct {
	absSource string
	relSource string
	// resolved absolute path of the target file
	absTarget string
	marker    string
	body      string
	comment   string // comment prefix, e.g. "#"
}

// NewFragment constructs a Fragment by reading and parsing the source file.
// homeDir is used to expand a leading ~ in the target path.
func NewFragment(absSource, relSource, homeDir string) (*Fragment, error) {
	raw, err := os.ReadFile(absSource)
	if err != nil {
		return nil, fmt.Errorf("read fragment %s: %w", absSource, err)
	}
	hdr, body, err := ParseFragmentHeader(string(raw))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", relSource, err)
	}
	target := expandHome(hdr.RawTarget, homeDir)
	target = os.ExpandEnv(target)
	target, err = filepath.Abs(target)
	if err != nil {
		return nil, err
	}
	comment := commentPrefix(target)
	return &Fragment{
		absSource: absSource,
		relSource: relSource,
		absTarget: target,
		marker:    hdr.Marker,
		body:      body,
		comment:   comment,
	}, nil
}

// Kind implements plan.Action.
func (f *Fragment) Kind() string { return "fragment" }

// Target implements plan.FileAction.
func (f *Fragment) Target() string { return f.absTarget }

// Source implements plan.FileAction.
func (f *Fragment) Source() string { return f.relSource }

// Describe implements plan.Action.
func (f *Fragment) Describe() string {
	return fmt.Sprintf("fragment %s [%s] <- %s", f.absTarget, f.marker, f.relSource)
}

// open marker: "<comment> >>> d0t:<marker> >>>"
func (f *Fragment) openMarker() string {
	return fmt.Sprintf("%s >>> d0t:%s >>>", f.comment, f.marker)
}

// close marker: "<comment> <<< d0t:<marker> <<<"
func (f *Fragment) closeMarker() string {
	return fmt.Sprintf("%s <<< d0t:%s <<<", f.comment, f.marker)
}

// managedBlock returns the full block as it should appear in the target.
func (f *Fragment) managedBlock() string {
	return f.openMarker() + "\n" + f.body + f.closeMarker() + "\n"
}

// Plan implements plan.Action.
func (f *Fragment) Plan(_ *plan.Context) (plan.Change, error) {
	_, err := os.Lstat(f.absTarget)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return plan.Change{Op: plan.OpCreate}, nil
		}
		return plan.Change{}, err
	}

	existing, err := os.ReadFile(f.absTarget)
	if err != nil {
		return plan.Change{}, err
	}
	current := extractBlock(string(existing), f.openMarker(), f.closeMarker())
	want := f.managedBlock()

	if current == "" {
		// Block not present — we need to append it.
		return plan.Change{Op: plan.OpCreate, Detail: "block will be appended"}, nil
	}
	if current == want {
		return plan.Change{Op: plan.OpNoOp}, nil
	}
	return plan.Change{Op: plan.OpUpdate}, nil
}

// Apply implements plan.Action.
func (f *Fragment) Apply(ctx *plan.Context) error {
	change, err := f.Plan(ctx)
	if err != nil {
		return err
	}
	if change.Op == plan.OpNoOp {
		ctx.Out.Verbose("ok      fragment %s [%s]", f.absTarget, f.marker)
		return nil
	}
	if ctx.DryRun {
		ctx.Out.Info("[dry-run] %s %s", change.Op, f.Describe())
		return nil
	}

	var existing string
	raw, err := os.ReadFile(f.absTarget)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err == nil {
		existing = string(raw)
	}

	var updated string
	block := f.managedBlock()
	if hasBlock(existing, f.openMarker(), f.closeMarker()) {
		updated = replaceBlock(existing, f.openMarker(), f.closeMarker(), block)
	} else {
		// Ensure file ends with a newline before appending.
		if existing != "" && !strings.HasSuffix(existing, "\n") {
			existing += "\n"
		}
		updated = existing + block
	}

	mode := fs.FileMode(0o644)
	if info, err := os.Stat(f.absTarget); err == nil {
		mode = info.Mode().Perm()
	}
	if err := fsutil.AtomicWrite(f.absTarget, []byte(updated), mode); err != nil {
		return fmt.Errorf("write fragment target %s: %w", f.absTarget, err)
	}
	ctx.Out.Info("%-7s %s", change.Op, f.Describe())
	return nil
}

// ---------------------------------------------------------------------------
// block helpers
// ---------------------------------------------------------------------------

// extractBlock returns the full block (open marker through close marker
// inclusive, plus trailing newline) if present, or "" if absent.
func extractBlock(content, open, close string) string {
	start := strings.Index(content, open)
	if start < 0 {
		return ""
	}
	end := strings.Index(content[start:], close)
	if end < 0 {
		return ""
	}
	end = start + end + len(close)
	// Include the trailing newline if present.
	if end < len(content) && content[end] == '\n' {
		end++
	}
	return content[start:end]
}

func hasBlock(content, open, close string) bool {
	return strings.Contains(content, open) && strings.Contains(content, close)
}

// replaceBlock swaps the existing block (from open through close) with the
// new block string. It replaces the first occurrence only.
func replaceBlock(content, open, close, newBlock string) string {
	start := strings.Index(content, open)
	if start < 0 {
		return content + newBlock
	}
	end := strings.Index(content[start:], close)
	if end < 0 {
		return content
	}
	end = start + end + len(close)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	return content[:start] + newBlock + content[end:]
}

// commentPrefix returns the appropriate comment character for a file based
// on its extension. Defaults to "#".
func commentPrefix(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".lua", ".sql":
		return "--"
	case ".vim", ".vimrc":
		return `"`
	case ".css", ".c", ".cpp", ".h", ".js", ".ts", ".go", ".java", ".rs":
		return "//"
	default:
		return "#"
	}
}

func expandHome(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
