# dev-null-tui

[![Go Reference](
  <https://pkg.go.dev/badge/github.com/eggplants/dev-null-tui.svg>
)](
  <https://pkg.go.dev/github.com/eggplants/dev-null-tui>
) [![ci](
  <https://github.com/eggplants/dev-null-tui/actions/workflows/ci.yml/badge.svg>
)](
  <https://github.com/eggplants/dev-null-tui/actions/workflows/ci.yml>
) [![release](
  <https://github.com/eggplants/dev-null-tui/actions/workflows/release.yml/badge.svg>
)](
  <https://github.com/eggplants/dev-null-tui/actions/workflows/release.yml>
) [![OpenAPI](
  <https://img.shields.io/badge/OpenAPI-docs-6BA539?logo=openapiinitiative&logoColor=white>
)](
  <https://egpl.dev/dev-null-tui/openapi/>
) [![dev-null.sh](
  <https://img.shields.io/badge/dev--null.sh-%40eggplants-black>
)](
  <https://dev-null.sh/@eggplants>
)

A terminal client for [dev-null.sh](https://dev-null.sh), the social network for developers, built with [tview](https://github.com/rivo/tview).

![demo](demo.gif)

## Install

```sh
# via the install script (checks the download against the release's checksums)
curl -fsSL https://raw.githubusercontent.com/eggplants/dev-null-tui/master/install.sh | sh

# via mise
mise use -g github:eggplants/dev-null-tui

# via go
go install github.com/eggplants/dev-null-tui/cmd/dn@latest

# via docker
docker run --rm -it -v ~/.config/devnull-tui:/root/.config/devnull-tui:ro ghcr.io/eggplants/dev-null-tui
```

## Usage

```sh
# approve this TUI's SSH key on the web
dn login

# check
dn whoami

# open the TUI
dn          
```

`login`, `whoami` and `commit` can be shortened to `l`, `w` and `c` (e.g. `dn c 'Hello world!'`). `dn config` shows the
TUI's settings and where each one comes from.

### Committing from the shell

`dn commit` signs and pushes the commit with an Ed25519 or RSA key from your SSH agent or `~/.ssh`.

```sh
# post
dn commit 'Hello world!'

# reply
dn commit -r 0be0130 'Agreed!'

# with image (JPEG/PNG/WebP, up to 10MB, 3 files)
dn commit --image shipped.png --image demo.webp 'Shipped!'
git log -1 --format=%s | dn commit
```

### Signing in

`dn` signs in the same way the /dev/null mobile app does. `login` creates an ed25519 key in `~/.config/devnull-tui/`
(or uses the one you pass with `-key`) and opens `https://dev-null.sh/mobile/authorize` in your browser. Once you
approve it there, the browser sends a one-time code back through a `devnull://` link and `dn` redeems it. From then on,
every request is signed with that key ([SSHSIG](https://github.com/openssh/openssh-portable/blob/master/PROTOCOL.sshsig),
namespace `devnull-api`).

To revoke it, go to [`Settings → Devices`](https://dev-null.sh/settings/devices).

On Linux, `login` registers `dn` as the desktop's handler for `devnull://` links. If you move the binary, run `login` again.

### Keys

The keys are the web's where it has them. The status bar shows the ones that work on what is selected, and `?` all of
them.

| Key | Action |
| --- | --- |
| `j` `k` `↓` `↑` | Next / previous (scrolls through long commits) |
| `gg` `G` | First / last |
| `^d` `^u` | Page down / up |
| `1`–`6` `Tab` | Following, Latest, Trending, Notifications, Starred, Profile |
| `g` then `l` `e` `p` `s` | Your log (Following), Trending, Profile, Settings, as on the web |
| `g` then `a` `n` `b` | Latest, Notifications, Starred |
| `/` `^k` | Go to an `@username`, a profile's or commit's link, or a hash on screen |
| `⏎` `l` (or click it again) | Open the thread; on the open commit, reply |
| `Esc` `h` `⌫` | Back |
| `c` | New commit |
| `r` / `q` | Reply / quote |
| `o` | Show / hide the replies under a commit |
| `s` | Star |
| `P` | Give a pastel (in pastéis mode) |
| `u` | Open the author's profile |
| `y` | Copy the link (over SSH too, with OSC 52) |
| `w` | Open in the browser |
| `i` | View the images |
| `D` | Delete your commit |
| `!` | Report a commit |
| `F` / `M` / `B` | Follow / mute / block (on a profile) |
| `T` | Next profile tab: commits, replies, followers, following |
| `f` / `e` | Filter notifications by kind / list everyone in a group |
| `R` | Refresh |
| `?` | Help |
| `Q` `^c` | Quit |

In the composer:

| Key | Action |
| --- | --- |
| `^s` `^⏎` | Commit (`^⏎` where the terminal tells it from `⏎`) |
| `Esc` | Close; a new commit's text is kept as a draft for the next `c` |
| `^e` | Write it in `$VISUAL` or `$EDITOR` |
| `^p` | Preview |
| `Tab` | Complete the `@mention` being typed, from the people on screen |
| `^o` | Attach an image (`Tab` completes the path); pasting or dropping an image file attaches it too |
| `^v` | Attach the image on the clipboard |
| `^r` | Drop the last image |

### Settings

`g s` in the TUI picks your color, turns pastéis mode on or off, takes your profile from GitHub again, and lists who
you mute and block. The theme follows the terminal's background unless you pick one.

`settings.json` next to the config takes these, and environment variables override it:

| Setting | Environment | Default | |
| --- | --- | --- | --- |
| `theme` | `DN_THEME` | `auto` | `auto`, `dark` or `light` |
| `graphics` | `DN_GRAPHICS` | `auto` | `auto`, `kitty`, `sixel` or `none` |
| `poll_interval` | `DN_POLL_INTERVAL` | `30s` | How often new commits and notifications are checked for, 10s to 10m |
| `mouse` | `DN_MOUSE` | `true` | |

## Limitations

The TUI can do whatever the [signed API](https://github.com/nunomaduro/dev-null.sh#signed-requests) can.

Features that only the web has are out of reach for now:
- search
- remotes (messages and orgs)
- polls (voting and creating them)
- pinning
- scheduled commits
- translations
- quote with images

And the `devnull://` handler only gets registered on Linux.

The signed API only accepts JSON. So a commit with images is sent the same way `dn commit --image` sends it, signed
with an SSH key on your GitHub account, and that key has to be in your SSH agent or `~/.ssh`.

## License

[MIT](LICENSE)
