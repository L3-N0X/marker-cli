# marker-cli

Convert PDFs to Markdown from the terminal — tables, formulas and images
included — using [MistralAI's OCR API](https://console.mistral.ai/api-keys).

This is a terminal port of the
[obsidian-marker](https://github.com/L3-N0X/obsidian-marker) plugin, without
the Obsidian dependency.

## Install

### Download a binary

Grab the archive for your platform from the
[latest release](https://github.com/L3-N0X/marker-cli/releases/latest) —
Linux, macOS and Windows, Intel and ARM. Each archive contains the binary and
shell completions.

**macOS / Linux**

```sh
tar xzf marker-cli_*.tar.gz
sudo install -Dm755 marker-cli /usr/local/bin/marker-cli
```

macOS blocks unsigned binaries the first time they run; clear the quarantine
flag with `xattr -d com.apple.quarantine /usr/local/bin/marker-cli`.

**Windows** — unzip and put `marker-cli.exe` anywhere on your `PATH`.

### Arch Linux

Available on the AUR as
[`marker-cli`](https://aur.archlinux.org/packages/marker-cli) (latest tagged
release) and [`marker-cli-git`](https://aur.archlinux.org/packages/marker-cli-git)
(builds from `main`):

```sh
yay -S marker-cli   # or: paru -S marker-cli
```

Without an AUR helper:

```sh
git clone https://aur.archlinux.org/marker-cli.git
cd marker-cli
makepkg -si
```

### Any Linux / macOS

Needs Go 1.26 or newer:

```sh
make
sudo make install              # /usr/local by default
make PREFIX=~/.local install   # or somewhere in your home
```

This installs the binary plus bash, zsh and fish completions.
`sudo make uninstall` removes them again.

### Just the binary

```sh
go build -o marker-cli .
```

## Getting started

Sign in once. Your key is validated against the API and stored in your
operating system's keyring — never in a config file:

```sh
marker-cli login
```

Then just run it:

```sh
marker-cli
```

That opens the interactive browser (see below). If you would rather stay on
the command line:

```sh
marker-cli convert -i paper.pdf -o notes/
```

## Interactive mode

```sh
marker-cli start            # browse the current directory
marker-cli start ~/Downloads
```

`start` (also what a bare `marker-cli` runs in a terminal) is a full-screen,
two-pane browser: PDFs on the left, conversion settings on the right.

```
╭─ FILES ───────────────────────────╮╭─ SETTINGS ──────────────╮
│ ❯ [✓] paper.pdf   1.2 MB  exists  ││     extract        all  │
│   [ ] thesis.pdf  4.8 MB          ││   ✓ assets-subfolder    │
│       archive/                    ││   ✗ metadata            │
╰───────────────────────────────────╯╰─────────────────────────╯
```

| Key | Does |
| --- | ---- |
| `↑` `↓` (or `k` `j`) | move the cursor |
| `space` | select / deselect the PDF under the cursor |
| `enter` | convert the selection here — or open the folder under the cursor |
| `f` | ask for a folder name first, then convert into it |
| `tab` | switch between the file list and the settings |
| `←` `→` | change the setting under the cursor |
| `s` | save the current settings as your defaults |
| `/` | filter the listing · `a` select all · `c` clear · `r` reload |
| `backspace` | go up a directory |
| `esc` | cancel a running conversion · `q` quit |

With nothing selected, `enter` converts whatever the cursor is on, so the
common case is two keystrokes. Files whose Markdown already exists are marked
`exists`; turn on the `force` setting to overwrite them.

Settings changed here apply to the session immediately. Press `s` to persist
them to the same config file `marker-cli config set` writes.

## Usage

```sh
marker-cli convert -i paper.pdf -o notes/            # notes/paper/paper.md + assets/
marker-cli convert -i paper.pdf -o notes/paper.md    # exactly that file + paper_assets/
marker-cli convert -i a.pdf -i b.pdf -o notes/       # batch
marker-cli convert *.pdf -o notes/                   # positional inputs work too
marker-cli -i paper.pdf -o notes/                    # `convert` is the default command
```

### Output layout

`-o` is interpreted by what it looks like:

| `-o` value  | Markdown written to  | Images written to |
| ----------- | -------------------- | ----------------- |
| `notes/`    | `notes/paper/paper.md` | `notes/paper/assets/` |
| `notes/x.md` | `notes/x.md`        | `notes/x_assets/` |

`--assets-subfolder=false` puts images next to the Markdown file instead.
Image links in the Markdown are rewritten to point at wherever the files
actually landed.

### Flags

| Flag | Default | Description |
| ---- | ------- | ----------- |
| `-i, --input` | — | PDF to convert; repeat for several |
| `-o, --output` | `.` | Output directory, or a path ending in `.md` |
| `--extract` | `all` | `all`, `text` or `images` |
| `--paginate` | `false` | Insert a horizontal rule between pages |
| `--image-limit` | `0` | Maximum images to extract (0 = no limit) |
| `--image-min-size` | `0` | Minimum image width/height (0 = no minimum) |
| `--assets-subfolder` | `true` | Put images in a separate assets folder |
| `--metadata` | `false` | Write metadata as YAML frontmatter |
| `--move-pdf` | `false` | Move the source PDF next to the Markdown |
| `--delete-original` | `false` | Delete the source PDF after conversion |
| `--delete-remote` | `false` | Delete the uploaded file from Mistral afterwards |
| `--force` | `false` | Overwrite existing Markdown files |
| `--no-tui` | `false` | Plain line output instead of the progress UI |
| `-v, --verbose` | `false` | Print each conversion stage |

### Defaults

Every conversion flag can be given a persistent default:

```sh
marker-cli config show
marker-cli config set extract text
marker-cli config set paginate true
marker-cli config path
```

Defaults live in `~/.config/marker-cli/config.json`. Flags override them.

## Credentials

Lookup order:

1. the OS keyring (`marker-cli login` writes here)
2. `$MISTRAL_API_KEY`

The environment variable makes the tool usable on headless machines and in CI,
where there is no Secret Service to talk to. `marker-cli logout` removes the
stored key. `marker-cli config show` prints which source is in use, never the
key itself.

`MISTRAL_BASE_URL` overrides the API root if you route through a proxy.

> [!NOTE]
> PDFs are uploaded to Mistral's servers to be processed, and are kept there
> for at least 24 hours. Pass `--delete-remote` to remove the upload as soon as
> the conversion finishes.

## Interactive bits

These are interactive, and all fall back to plain output when stdout is not a
terminal:

- `marker-cli start` — the two-pane file browser described above.
- `marker-cli login` — masked key entry with live validation before saving.
- `marker-cli convert` — a spinner and progress bar per file.

Piping (`marker-cli convert ... | tee log`) automatically switches to plain
line logging on stderr, leaving stdout clean.

## Development

```sh
make check      # go vet ./... && go test ./...
make snapshot   # build every release archive into dist/, publishing nothing
```

Releases are cut by pushing a tag — GoReleaser builds and uploads the
cross-platform archives from GitHub Actions:

```sh
git tag -a v0.1.0 -m "v0.1.0" && git push origin v0.1.0
```

Layout:

| Path | Purpose |
| ---- | ------- |
| `internal/cmd` | Cobra commands and flag wiring |
| `internal/converter` | Backend-agnostic `Converter` interface |
| `internal/converter/mistral` | MistralAI OCR REST client and converter |
| `internal/output` | Turning a result into files on disk |
| `internal/secrets` | OS keyring access |
| `internal/config` | Persisted non-secret defaults |
| `internal/tui` | Bubble Tea views: file browser, login, progress |
| `packaging/arch` | PKGBUILDs for the AUR (`marker-cli`, `marker-cli-git`) |
| `.goreleaser.yaml` | Cross-platform release build |

Adding another backend (Datalab, self-hosted Marker, the Python API) means
implementing `converter.Converter` and adding an entry to `providers` in
`internal/cmd/provider.go`.

## Acknowledgements

- [Marker](https://github.com/VikParuchuri/marker) — the model behind the idea
- [obsidian-marker](https://github.com/L3-N0X/obsidian-marker) — the plugin this ports
- [MistralAI](https://mistral.ai/) — the OCR API
- [Charm](https://charm.sh/) — Bubble Tea, Bubbles and Lip Gloss
