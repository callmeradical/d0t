package primitive

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"d0t/internal/fsutil"
	"d0t/internal/plan"
	"d0t/internal/secrets"
	"d0t/internal/vars"
)

// BuiltinVars are the host-level variables injected into every template
// automatically. They are distinct from user-defined vars.Map so that
// .Host, .OS etc. are always present and can't be shadowed by a typo in
// vars.toml.
type BuiltinVars struct {
	Host    string
	OS      string
	Arch    string
	User    string
	Home    string
	// Secrets is the backend used to resolve {{ secret "..." }} calls in
	// templates. Nil means no backend is configured — any secret() call
	// will return an error at render time.
	Secrets secrets.Backend
}

// templateData is the root object visible inside a .tmpl file.
type templateData struct {
	BuiltinVars        // embedded: .Host, .OS, .Arch, .User, .Home
	Vars        vars.Map // .Vars.key
}

// Template renders a Go text/template source file and writes the result to
// Target. Change detection is by content hash of the rendered output, so
// it behaves like Copy for the purposes of plan/status/diff.
type Template struct {
	source   string
	target   string
	relSrc   string
	userVars vars.Map
	builtins BuiltinVars
	mode     fs.FileMode
}

// NewTemplate constructs a Template action.
// mode defaults to 0o644 when zero.
func NewTemplate(source, target, relSource string, userVars vars.Map, builtins BuiltinVars) *Template {
	return &Template{
		source:   source,
		target:   target,
		relSrc:   relSource,
		userVars: userVars,
		builtins: builtins,
		mode:     0o644,
	}
}

// Kind implements plan.Action.
func (t *Template) Kind() string { return "template" }

// Target implements plan.FileAction.
func (t *Template) Target() string { return t.target }

// Source implements plan.FileAction.
func (t *Template) Source() string { return t.relSrc }

// Describe implements plan.Action.
func (t *Template) Describe() string {
	return fmt.Sprintf("template %s <- %s", t.target, t.relSrc)
}

// render compiles and executes the template, returning the rendered bytes.
func (t *Template) render() ([]byte, error) {
	src, err := os.ReadFile(t.source)
	if err != nil {
		return nil, fmt.Errorf("read template %s: %w", t.source, err)
	}

	name := filepath.Base(t.source)
	tmpl, err := template.New(name).Funcs(funcMap(t.builtins.Secrets)).Parse(string(src))
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", t.relSrc, err)
	}

	data := templateData{
		BuiltinVars: t.builtins,
		Vars:        t.userVars,
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render template %s: %w", t.relSrc, err)
	}
	return buf.Bytes(), nil
}

// Plan implements plan.Action.
func (t *Template) Plan(_ *plan.Context) (plan.Change, error) {
	rendered, err := t.render()
	if err != nil {
		return plan.Change{}, err
	}
	wantHash := fsutil.HashBytes(rendered)

	info, err := os.Lstat(t.target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return plan.Change{Op: plan.OpCreate}, nil
		}
		return plan.Change{}, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return plan.Change{Op: plan.OpConflict, Detail: "target is a symlink"}, nil
	}
	if info.IsDir() {
		return plan.Change{Op: plan.OpConflict, Detail: "target is a directory"}, nil
	}
	gotHash, err := fsutil.HashFile(t.target)
	if err != nil {
		return plan.Change{}, err
	}
	if gotHash == wantHash {
		return plan.Change{Op: plan.OpNoOp}, nil
	}
	return plan.Change{Op: plan.OpUpdate}, nil
}

// Apply implements plan.Action.
func (t *Template) Apply(ctx *plan.Context) error {
	rendered, err := t.render()
	if err != nil {
		return err
	}

	change, err := t.Plan(ctx)
	if err != nil {
		return err
	}
	switch change.Op {
	case plan.OpNoOp:
		ctx.Out.Verbose("ok      template %s", t.target)
		return nil
	case plan.OpConflict:
		if !ctx.Force && !ctx.Adopt {
			return fmt.Errorf("template %s: %s (use --adopt or --force)", t.target, change.Detail)
		}
		if !ctx.DryRun {
			if ctx.Adopt {
				backup := fsutil.BackupPath(t.target)
				if err := os.Rename(t.target, backup); err != nil {
					return fmt.Errorf("backup %s: %w", t.target, err)
				}
				ctx.Out.Info("backup  %s -> %s", t.target, backup)
			} else {
				if err := os.Remove(t.target); err != nil {
					return err
				}
			}
		}
	}

	if ctx.DryRun {
		ctx.Out.Info("[dry-run] %s %s", change.Op, t.Describe())
		return nil
	}
	if err := fsutil.AtomicWrite(t.target, rendered, t.mode.Perm()); err != nil {
		return fmt.Errorf("write %s: %w", t.target, err)
	}
	ctx.Out.Info("%-7s %s", change.Op, t.Describe())
	return nil
}

// funcMap returns the template functions available in .tmpl files.
// backend may be nil; if so, any call to secret() will return an error.
func funcMap(backend secrets.Backend) template.FuncMap {
	return template.FuncMap{
		// secret resolves a secret by key using the configured backend.
		// Key prefix selects the backend (op://, keychain://, env://, pass://)
		// or falls back to the default backend for bare keys.
		"secret": func(key string) (string, error) {
			if backend == nil {
				return "", fmt.Errorf("secret(%q): no secrets backend configured — add [secrets] backend = \"op\" (or keychain/env/pass) to d0t.toml", key)
			}
			return backend.Get(key)
		},
		// default returns the value if non-empty, otherwise the fallback.
		// Accepts any first arg so missing map keys (which arrive as the
		// zero reflect.Value, not "") are handled correctly.
		"default": func(val any, fallback string) string {
			s, ok := val.(string)
			if !ok || s == "" {
				return fallback
			}
			return s
		},
		// env looks up an environment variable.
		"env": os.Getenv,
		// lower / upper / trim are string helpers.
		"lower": strings.ToLower,
		"upper": strings.ToUpper,
		"trim":  strings.TrimSpace,
		// joinPath joins path elements with the OS separator.
		"joinPath": filepath.Join,
	}
}
