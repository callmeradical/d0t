# d0t

A dotfiles manager with an explicit resource manifest. Every managed file,
hook, and package is declared in a `d0tfile` — nothing is inferred from
directory layout or filename suffixes. One clear source of truth per profile.
A single static binary with no runtime dependencies.

```
d0t plan        # see what would change
d0t apply       # converge your machine to the repo
d0t status      # show drift since last apply
d0t adopt ~/.zshrc   # pull an existing file into the repo
```

## Install

```sh
go install github.com/callmeradical/d0t/cmd/d0t@latest
```

Or build from source:

```sh
git clone https://github.com/callmeradical/d0t ~/.d0t-src
cd d0t
go build -o ~/.local/bin/d0t ./cmd/d0t
```

## Quick start

```sh
# Scaffold a new repo at ~/.d0t (creates directory layout + starter d0tfile)
d0t init

# Move an existing config into the repo and replace it with a symlink
d0t adopt ~/.zshrc           # → ~/.d0t/base/home/.zshrc
d0t adopt ~/.gitconfig

# Edit the d0tfile to declare what you want managed
$EDITOR ~/.d0t/base/d0tfile

# Preview what apply would do
d0t plan

# Apply — creates symlinks, renders templates, runs hooks, installs packages
d0t apply
```

The repo defaults to `~/.d0t`. Override with `--repo` or `$D0T_REPO`.

## Repo layout

```
~/.d0t/
├── d0t.toml              # optional repo config
├── base/                 # applied on every machine
│   ├── d0tfile           # resource manifest — the source of truth
│   ├── home/             # source files that map to $HOME
│   │   └── .zshrc
│   ├── xdg/              # source files that map to ~/.config
│   │   └── nvim/
│   │       └── init.lua
│   ├── fragments/        # blocks to inject into files d0t doesn't own
│   │   └── path.fragment
│   ├── hooks/            # lifecycle scripts referenced from d0tfile
│   │   └── post-apply.sh
│   └── vars.toml         # template variables for .tmpl files
├── os/
│   ├── darwin/           # applied on macOS only
│   └── linux/            # applied on Linux only
└── hosts/
    └── work-mbp/         # applied when hostname matches
```

Each profile directory contains a `d0tfile` that declares what is managed.
Source files (in `home/`, `xdg/`, etc.) are just storage — they are not
discovered automatically. Only what is declared in `d0tfile` is managed.

Profiles layer in order: `base` → `os/<os>` → `hosts/<hostname>`. For any
given target path, the last profile that provides a source wins. Hooks and
packages accumulate across all active profiles.

### Filesystem roots

| Directory | Maps to | Notes |
|---|---|---|
| `home/` | `$HOME` | |
| `xdg/` | `$XDG_CONFIG_HOME` (default `~/.config`) | |
| `etc/` | `/etc` | requires sudo |
| `fragments/` | target declared per-file | see fragment primitive |

Custom roots can be added in `d0t.toml`:

```toml
[roots]
caches = "${XDG_CACHE_HOME:-$HOME/.cache}"
```

## Primitives

There are six primitives, each declared with a keyword in `d0tfile`:

| Keyword | What it does |
|---|---|
| `link` | Symlink the source file into the target location |
| `copy` | Copy the source bytes to the target (use for chmod-sensitive files) |
| `tmpl` / `template` | Render source as Go template, write to target |
| `fragment` | Inject a managed block into a file d0t doesn't own |
| `hook` | Run a lifecycle script (pre-apply, post-apply, pre-remove, post-remove) |
| `brew` / `cask` / `tap` / `apt` / `mas` | Install packages |

### link

Creates a symlink at the target pointing to the source file in the repo. This
is the default — edits in the repo are live immediately.

If a file already exists at the target and is not the expected symlink, `apply`
will fail. Use `--adopt` to back up the existing file and replace it, or
`--force` to overwrite it without a backup.

### copy

Copies the file bytes to the target. Use this for files that must not be
symlinks — apps that rewrite their own config, or files with restricted
permissions like `~/.ssh/config`. The source file's mode bits are preserved, so
`chmod 600` the source to get a `600` target.

### template

Renders the source with Go's `text/template` and writes the result to the
target. Change detection is by content hash, so re-applying an unchanged
template is a no-op.

**Built-in variables:**

```
.Host       hostname (short, no domain)
.OS         darwin / linux / windows
.Arch       amd64 / arm64
.User       current username
.Home       $HOME
```

**User variables** come from `vars.toml` in each active profile, merged in
profile order. Access them as `.Vars.<key>`:

```toml
# base/vars.toml
email = "you@example.com"
editor = "nvim"
```

```
# base/home/.gitconfig.tmpl
[user]
  email = {{ .Vars.email }}
  name  = {{ .User }}
[core]
  editor = {{ default .Vars.editor "vim" }}
```

**Built-in functions:** `env`, `default`, `lower`, `upper`, `trim`, `joinPath`, `secret`.

#### secret()

Resolves a secret at render time from a configured backend. Secrets never
touch the repo.

```
# base/home/.gitconfig.tmpl
[github]
  token = {{ secret "op://Personal/GitHub/token" }}

[user]
  email      = {{ secret "op://Personal/Email/address" }}
  signingkey = {{ secret "keychain://gpg/signing-key" }}
```

Key prefix selects the backend explicitly:

| Prefix | Backend |
|---|---|
| `op://Vault/Item/Field` | 1Password (`op read`) |
| `keychain://service/account` | macOS Keychain (`security`) |
| `env://VAR_NAME` | Environment variable |
| `pass://path/to/secret` | pass |
| (bare key) | default backend from `d0t.toml` |

Configure the default backend in `d0t.toml`:

```toml
[secrets]
backend = "op"        # 1Password — default for bare keys
# backend = "keychain"
# backend = "env"
# backend = "pass"
```

Secrets are cached in-process per apply run — `op read` fires at most once
per unique key regardless of how many templates reference it.

### fragment

Injects a managed block into a file that d0t does not own. Useful for appending
to system files or files shared with other tools.

The source file starts with a header declaring the target and a unique marker:

```sh
# d0t: target=~/.zshrc marker=local-bin-path
export PATH="$HOME/.local/bin:$PATH"
```

`apply` appends the block (or replaces it if it already exists):

```
# >>> d0t:local-bin-path >>>
export PATH="$HOME/.local/bin:$PATH"
# <<< d0t:local-bin-path <<<
```

`remove` deletes the block by marker, leaving the rest of the file intact.
Comment style is auto-detected from the target file extension (`#` default,
`//` for Go/JS/etc., `--` for Lua/SQL).

### exec (hooks)

Scripts in `hooks/` run at lifecycle phases. The phase is the filename prefix:

```
hooks/
├── pre-apply.sh
├── post-apply-10-rehash.sh
├── post-apply-20-set-defaults.sh
└── post-remove.sh
```

Scripts within a phase run in lexical order. A non-zero exit aborts apply.
Append `.optional.` to the name to continue on failure:

```
hooks/post-apply.optional.sh   # failure is logged but not fatal
```

**Environment variables available to hooks:**

| Variable | Value |
|---|---|
| `D0T_REPO` | absolute path to the repo |
| `D0T_HOST` | hostname |
| `D0T_OS` | `darwin` / `linux` |
| `D0T_PROFILE` | name of the profile this hook belongs to |
| `D0T_DRY_RUN` | `true` when running under `--dry-run` |

### pkg

Packages declared in `packages.toml` are installed as part of `apply`.
Managers not present on the host are silently skipped (set `strict_pkg = true`
in `d0t.toml` to make them an error instead).

```toml
[brew]
taps     = ["homebrew/cask-fonts"]
formulae = ["ripgrep", "fd", "bat", "neovim"]
casks    = ["ghostty", "raycast"]

[apt]
packages = ["ripgrep", "fd-find", "bat", "neovim"]

[[mas.apps]]
id   = 497799835
name = "Xcode"
```

Packages accumulate across profiles — a formula in `base/packages.toml` and
another in `hosts/work-mbp/packages.toml` are both installed, with duplicates
silently deduped.

## Commands

```
d0t apply    Converge the machine to the current repo state.
d0t plan     Show what apply would do without changing anything.
d0t status   Show managed targets and their current state.
d0t diff     Show content diff for templates and copies.
d0t remove   Remove everything d0t manages (uses tracked state).
d0t adopt    Move an existing config file into the repo and replace it with
             a symlink. Defaults to base/home/<relative-to-home>.
d0t init     Scaffold a new repo with the standard directory layout.
d0t doctor   Sanity-check the repo and host environment.
```

**Global flags:**

```
--repo PATH      Path to the dotfiles repo (default: $D0T_REPO or ~/.d0t)
--profile LIST   Comma-separated profile override (e.g. base,work,hosts/mbp)
--dry-run        Show what would happen without modifying the filesystem
--verbose        Show no-op actions and extra detail
--yes            Skip confirmation prompts
```

**Apply-specific flags:**

```
--no-pkg     Skip package installation
--no-hooks   Skip exec hooks
--force      Overwrite unmanaged files at target paths
--adopt      Back up unmanaged files before overwriting
```

## Directory symlinking

By default d0t links individual files. To link an entire directory as a single
symlink (useful for tools that rewrite their own config directory), add a
`.d0tdir` marker file inside the directory in the repo:

```sh
touch ~/.d0t/base/home/.ssh/.d0tdir
```

`apply` will create `~/.ssh → <repo>/base/home/.ssh` as a single symlink and
will not descend into the directory.

## Profiles

Profiles let you layer machine-specific or OS-specific configuration on top of
a common base without duplicating files.

```
base/             # always applied
os/darwin/        # macOS only
os/linux/         # Linux only
hosts/work-mbp/   # only when hostname == "work-mbp"
```

Override the profile stack entirely in `d0t.toml`:

```toml
profiles = ["base", "os/darwin", "work", "hosts/work-mbp"]
```

Or at apply time:

```sh
d0t --profile base,work apply
```

For any target path, the **last** active profile that provides a source wins.
Hooks and packages from all active profiles are accumulated and all run/install.

## d0tfile

Every profile is driven by a `d0tfile`. This is how d0t knows what to manage —
source files sitting in `home/` or `xdg/` are ignored unless they are declared.
Think of it like a Makefile or a Chef recipe: explicit, auditable, no magic.

```
# base/d0tfile

link   home/.zshrc
link   home/.zshenv
link   xdg/nvim

copy   home/.ssh/config    mode=0600

tmpl   home/.gitconfig

fragment fragments/path.fragment

hook pre-apply  hooks/pre-apply.sh
hook post-apply hooks/post-apply.sh  optional

brew  ripgrep neovim bat fzf lazygit starship tmux
cask  ghostty raycast
tap   homebrew/cask-fonts
mas   497799835  Xcode
apt   ripgrep neovim
```

### Syntax

One declaration per line. `#` starts a comment (inline or full-line). Blank
lines are ignored.

**File resources**

```
link     <source>              [target=<path>]
copy     <source>              [target=<path>] [mode=<octal>]
tmpl     <source>              [target=<path>]
template <source>              [target=<path>]
fragment <source>              [target=<path>] [marker=<id>]
```

`source` is a path relative to the profile directory. The target is inferred
from the root prefix (`home/` → `$HOME`, `xdg/` → `~/.config`) unless
overridden with `target=`.

**Hooks**

```
hook <phase> <script> [optional]
```

`phase` is one of `pre-apply`, `post-apply`, `pre-remove`, `post-remove`.
The `optional` flag means a non-zero exit is logged but does not abort apply.

**Packages**

```
brew  <formula> [formula ...]
cask  <cask> [cask ...]
tap   <tap>
apt   <package> [package ...]
mas   <id> <name>
```

Multiple packages on one line. MAS name can contain spaces.

Profiles without a `d0tfile` are silently skipped — they contribute nothing to
the plan. Later profiles always win on a per-target-path basis.

## Migrating from stow

The layouts are similar — stow packages map directly to d0t profile roots.

| stow | d0t |
|---|---|
| `nvim/.config/nvim/` | `base/xdg/nvim/` |
| `zsh/.zshrc` | `base/home/.zshrc` |
| `ssh/.ssh/config` | `base/home/.ssh/config.copy` (use copy for chmod 600) |
| `setup.sh` post-stow commands | `base/hooks/post-apply.sh` |
| Brewfile | `base/packages.toml` |

The fastest migration path using a `d0tfile`:

```sh
d0t init

# Copy your stow trees into the repo
cp -r ~/path/to/dotfiles/zsh/.zshrc    ~/.d0t/base/home/.zshrc
cp -r ~/path/to/dotfiles/nvim/.config/nvim  ~/.d0t/base/xdg/nvim
cp -r ~/path/to/dotfiles/ssh/.ssh/config    ~/.d0t/base/home/.ssh/config

# Write a d0tfile
cat > ~/.d0t/base/d0tfile <<'EOF'
link   home/.zshrc
link   xdg/nvim

copy   home/.ssh/config  mode=0600
EOF

d0t plan                   # verify before applying
d0t apply --adopt          # back up any conflicts, then apply
```

Or use `adopt` to pull files from their live locations one at a time:

```sh
d0t adopt ~/.zshrc         # moves file into repo, replaces with symlink
d0t adopt ~/.config/nvim --dest xdg/nvim
d0t plan && d0t apply --adopt
```

## State

d0t tracks every managed target in `~/.local/state/d0t/state.json`
(respects `$XDG_STATE_HOME`). The state file records the primitive, source
path, and content hash for each target. It is used by `remove` to safely undo
what `apply` did, and by `status` to detect files that have drifted from what
d0t last wrote.

The state file is machine-local and should not be committed to the repo.
