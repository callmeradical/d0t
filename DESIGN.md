# d0t — design

A small, opinionated dotfiles manager. The repo layout *is* the configuration.
The DSL is kept to the bare minimum needed for things filesystem layout cannot
express (hooks, packages, template variables).

## Goals

- **Convention over configuration.** The directory layout maps directly to the
  filesystem. You shouldn't need a manifest to manage a `.zshrc`.
- **Layered profiles.** `base` → `os/<os>` → `hosts/<hostname>`. Later layers
  win on a per-target-path basis.
- **Six primitives.** `link`, `copy`, `template`, `fragment`, `exec`, `pkg`.
- **Idempotent.** Re-running `apply` is safe and produces no diff.
- **Reversible.** Everything d0t writes is tracked; `d0t remove` undoes it.
- **One static binary.** No runtime dependencies on the target machine.

## Repository layout

```
<repo>/
├── d0t.toml                 # optional: profile order, custom roots, defaults
├── base/                    # always applied
│   ├── home/                # → $HOME
│   │   ├── .zshrc
│   │   ├── .gitconfig.tmpl  # template; rendered to ~/.gitconfig
│   │   └── .ssh/
│   │       └── config.copy  # copied (not linked) to ~/.ssh/config
│   ├── xdg/                 # → ${XDG_CONFIG_HOME:-~/.config}
│   │   └── nvim/init.lua
│   ├── etc/                 # → /etc (requires sudo)
│   ├── fragments/           # managed-block edits to files d0t doesn't own
│   │   └── path-export.sh
│   ├── hooks/               # lifecycle scripts, run in lex order per phase
│   │   ├── pre-apply.sh
│   │   └── post-apply.sh
│   ├── packages.toml        # declared packages
│   └── vars.toml            # template variables for this layer
├── os/
│   ├── darwin/              # applied when GOOS == darwin
│   └── linux/
└── hosts/
    └── <hostname>/          # applied when hostname matches
```

A profile is just a directory with the same internal shape (`home/`, `xdg/`,
`etc/`, `fragments/`, `hooks/`, `packages.toml`, `vars.toml`).

### Filesystem roots

Top-level directories inside a profile map to filesystem roots:

| dir       | maps to                                  | notes                |
|-----------|------------------------------------------|----------------------|
| `home/`   | `$HOME`                                  |                      |
| `xdg/`    | `${XDG_CONFIG_HOME:-$HOME/.config}`      |                      |
| `etc/`    | `/etc`                                   | needs sudo           |
| `root/`   | `/`                                      | needs sudo, escape   |

Custom roots can be declared in `d0t.toml`:

```toml
[roots]
caches = "${XDG_CACHE_HOME:-$HOME/.cache}"
```

## Primitives

The primitive applied to a source file is determined by **filename suffix**.
The suffix is stripped from the target path. This keeps the layout convention
intact and avoids per-file manifest entries.

| suffix       | primitive  | target name                   |
|--------------|------------|-------------------------------|
| (none)       | `link`     | same as source                |
| `.copy`      | `copy`     | suffix stripped               |
| `.tmpl`      | `template` | suffix stripped               |
| `.fragment`  | `fragment` | declared in file frontmatter  |

Directories are walked; `link` is applied at the *file* level by default. A
directory containing a `.d0tdir` marker file is itself linked as a single
symlink (useful for tools that rewrite their own config dir, e.g. `~/.ssh`).

### `link`

Creates a symlink from the target path to the source file. Default primitive.
Idempotent. If the target exists and is not the expected symlink, `apply` fails
unless `--adopt` (back up + replace) or `--force` is given.

### `copy`

Renders the file's bytes into the target. Used for files that must not be
symlinks (apps that rewrite their config, files with restricted permissions
like `~/.ssh/config`). Mode is preserved from the source.

### `template`

Source is rendered with Go's `text/template` and written to the target. Vars
come from `vars.toml` files in each active profile, merged in profile order.

Built-in vars:

```
.Host       hostname
.OS         runtime.GOOS
.Arch       runtime.GOARCH
.User       current user name
.Home       $HOME
.Env.<KEY>  environment variable lookup
```

Built-in funcs: `env`, `default`, `lower`, `upper`, `trim`, `joinPath`,
`secret` (pluggable; resolves via configured backend, e.g. `op`/`pass`).

### `fragment`

Inserts a managed block into a file d0t does not own (e.g. appending to a
system-installed `/etc/zshrc`). The fragment file declares its target and a
unique marker in a frontmatter header:

```
# d0t: target=~/.zshrc marker=path-export
export PATH="$HOME/.local/bin:$PATH"
```

The rendered block in the target file looks like:

```
# >>> d0t:path-export >>>
export PATH="$HOME/.local/bin:$PATH"
# <<< d0t:path-export <<<
```

`apply` replaces an existing block with the same marker; `remove` deletes the
block by marker. Comment style is auto-detected by target extension (defaults
to `#`).

### `exec`

Hook scripts in `hooks/` are executed at lifecycle phases. Phase is determined
by filename prefix:

```
hooks/
├── pre-apply.sh
├── post-apply-10-reload-shell.sh
├── post-apply-20-rebuild-bat-cache.sh
├── pre-remove.sh
└── post-remove.sh
```

Scripts run in lexical order within a phase, with `cwd` set to the profile
root. Environment includes `D0T_HOST`, `D0T_OS`, `D0T_PROFILE`, `D0T_REPO`,
`D0T_DRY_RUN`. A non-zero exit aborts apply unless the script name contains
`.optional.`.

### `pkg`

Declared in `packages.toml` per profile:

```toml
[brew]
formulae = ["ripgrep", "fd", "bat"]
casks    = ["ghostty"]
taps     = ["homebrew/cask-fonts"]

[apt]
packages = ["ripgrep", "fd-find", "bat"]

[mas]
apps = [{ id = 497799835, name = "Xcode" }]
```

`apply` invokes the right manager idempotently. Managers that aren't installed
on the host are skipped with a warning unless `strict_pkg = true` in
`d0t.toml`.

## Profile resolution

Default profile order:

1. `base`
2. `os/<runtime.GOOS>`
3. `hosts/<hostname>`

Override or extend in `d0t.toml`:

```toml
profiles = ["base", "os/darwin", "work", "hosts/work-mbp"]
```

For each target path, the *last* profile that produces a source wins. Hooks
and packages are **accumulated** across profiles (not overridden).

## State

`${XDG_STATE_HOME:-~/.local/state}/d0t/state.json` records every managed
target with its primitive, source path, content hash, and (for fragments) the
marker. Used to:

- detect drift (file mutated outside d0t)
- safely remove targets that no longer exist in the repo
- avoid clobbering files d0t didn't put there

## Commands

```
d0t apply             converge target state
d0t plan              show what apply would do (dry run)
d0t status            show drift, missing, extra
d0t diff [path]       show content diff for templates/copies
d0t remove            tear down everything d0t manages
d0t adopt <path>      move an existing config into the repo and replace with link
d0t init              scaffold a new repo
d0t doctor            sanity-check repo and host
```

Global flags: `--repo`, `--profile` (override resolution), `--dry-run`,
`--verbose`, `--yes`.

## Non-goals

- Templating engine pluralism. Just `text/template`.
- Embedded scripting language. Hooks are real shell scripts.
- Full configuration management (à la NixOS). d0t does dotfiles and a thin
  package layer; that's it.
- Encrypted secrets in-tree. Use a `secret` template func that delegates to
  an external backend.

## Open questions

- Should `copy` files be `chmod`ed back to source mode, or always `0600` for
  paths under `~/.ssh`? (Lean: preserve source mode, warn on world-readable
  ssh files.)
- Should `fragment` markers be globally unique or scoped to target file?
  (Lean: scoped to target file.)
- Should `pkg` install be opt-in (`d0t apply --pkg`) or part of `apply`?
  (Lean: part of apply, but `--no-pkg` flag exists.)
