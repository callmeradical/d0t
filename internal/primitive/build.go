package primitive

import (
	"fmt"
	"path/filepath"

	"d0t/internal/manifest"
	"d0t/internal/plan"
	"d0t/internal/profile"
	"d0t/internal/repo"
	"d0t/internal/secrets"
	"d0t/internal/vars"
)

// NewProfileReaderBuilder returns the single plan.ProfileReaderBuilder used
// by d0t. It pre-loads merged vars across all resolved profiles, then returns
// a reader that parses each profile's d0tfile and converts declarations to
// actions.
//
// This replaces the previous ActionFactory / FactoryBuilder / HookCollector /
// PkgLoader / ManifestReader five-callback system.
func NewProfileReaderBuilder() plan.ProfileReaderBuilder {
	return func(profs []profile.Profile, r *repo.Repo) (plan.ProfileReader, error) {
		// Load vars once across the full profile stack so that later profiles
		// can override earlier ones before any template is rendered.
		v, err := vars.Load(profs)
		if err != nil {
			return nil, fmt.Errorf("load vars: %w", err)
		}
		secretsBackend, err := secrets.NewFromConfig(r.Config.Secrets.Backend, nil)
		if err != nil {
			return nil, fmt.Errorf("secrets backend: %w", err)
		}
		b := BuiltinVars{
			Host:    r.Hostname,
			OS:      r.OS,
			Arch:    r.Arch,
			User:    r.User,
			Home:    r.Home,
			Secrets: secretsBackend,
		}
		return func(profilePath, profileName string, roots map[string]string) ([]plan.FileAction, []plan.Action, []plan.Action, error) {
			decls, err := manifest.Load(profilePath)
			if err != nil {
				return nil, nil, nil, err
			}
			if decls == nil {
				return nil, nil, nil, nil // no d0tfile — skip
			}

			var files []plan.FileAction
			for _, d := range decls {
				fd, ok := d.(*manifest.FileDecl)
				if !ok {
					continue
				}
				action, err := FromDeclaration(fd, profilePath, profileName, roots, v, b)
				if err != nil {
					return nil, nil, nil, fmt.Errorf("%s: %w", fd.Source, err)
				}
				files = append(files, action)
			}

			hooks := HooksFromDeclarations(decls, profilePath, profileName)

			rawPkgs := PkgActionsFromDeclarations(decls)
			pkgs := make([]plan.Action, len(rawPkgs))
			for i, a := range rawPkgs {
				pkgs[i] = a
			}

			return files, hooks, pkgs, nil
		}, nil
	}
}

// FromDeclaration converts a manifest.FileDecl into a FileAction, resolving
// the target path against the supplied root map.
func FromDeclaration(decl *manifest.FileDecl, profilePath, profileName string, roots map[string]string, v vars.Map, b BuiltinVars) (plan.FileAction, error) {
	absSource := filepath.Join(profilePath, decl.Source)
	relSource := filepath.Join(profileName, decl.Source)

	var target string
	if decl.Target != "" {
		target = expandHome(decl.Target, b.Home)
	} else {
		root, rel, err := splitRoot(decl.Source, roots)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", decl.Source, err)
		}
		target = filepath.Join(root, rel)
	}

	switch decl.Kind {
	case "link":
		return NewLink(absSource, target, relSource), nil
	case "copy":
		mode := decl.Mode
		if mode == 0 {
			mode = 0o644
		}
		return NewCopy(absSource, target, relSource, mode), nil
	case "template":
		return NewTemplate(absSource, target, relSource, v, b), nil
	case "fragment":
		return NewFragment(absSource, relSource, b.Home)
	default:
		return nil, fmt.Errorf("unknown kind %q", decl.Kind)
	}
}

// HooksFromDeclarations converts manifest HookDecls to Hook plan.Actions.
func HooksFromDeclarations(decls []manifest.Declaration, profilePath, profileName string) []plan.Action {
	var out []plan.Action
	for _, d := range decls {
		hd, ok := d.(*manifest.HookDecl)
		if !ok {
			continue
		}
		absScript := filepath.Join(profilePath, hd.Script)
		h := NewHook(filepath.Base(hd.Script), absScript, profileName, hd.Phase)
		h.Optional = hd.Optional
		out = append(out, h)
	}
	return out
}

// PkgActionsFromDeclarations converts manifest PkgDecls to PkgActions.
func PkgActionsFromDeclarations(decls []manifest.Declaration) []*PkgAction {
	m := &PkgManifest{}
	for _, d := range decls {
		pd, ok := d.(*manifest.PkgDecl)
		if !ok {
			continue
		}
		switch pd.Manager {
		case "brew":
			m.Brew.Formulae = append(m.Brew.Formulae, pd.Items...)
		case "cask":
			m.Brew.Casks = append(m.Brew.Casks, pd.Items...)
		case "tap":
			m.Brew.Taps = append(m.Brew.Taps, pd.Items...)
		case "apt":
			m.Apt.Packages = append(m.Apt.Packages, pd.Items...)
		case "mas":
			for _, a := range pd.MasApps {
				m.Mas.Apps = append(m.Mas.Apps, MasApp{ID: a.ID, Name: a.Name})
			}
		}
	}
	return ActionsFromManifest(m)
}

// splitRoot separates "home/.zshrc" into rootPath ($HOME) and ".zshrc".
func splitRoot(source string, roots map[string]string) (rootPath, rel string, err error) {
	for i, c := range source {
		if c == '/' || c == '\\' {
			rootName := source[:i]
			remainder := source[i+1:]
			rp, ok := roots[rootName]
			if !ok {
				return "", "", fmt.Errorf("unknown root %q (want home, xdg, etc, root, or a custom root)", rootName)
			}
			if rp == "__fragments__" {
				// Fragment targets come from their own frontmatter.
				return "/", remainder, nil
			}
			return rp, remainder, nil
		}
	}
	return "", "", fmt.Errorf("source %q has no root prefix (expected home/, xdg/, etc.)", source)
}
