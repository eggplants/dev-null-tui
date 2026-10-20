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
)

A terminal client for [dev-null.sh](https://dev-null.sh), the social network for developers, built with [tview](https://github.com/rivo/tview).

![demo](demo.gif)

## Install

```sh
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

| Key | Action |
| --- | --- |
| `j` `k` `↓` `↑` | Next / previous (scrolls through long commits) |
| `g` `G` | First / last |
| `^d` `^u` | Page down / up |
| `1`–`6` `Tab` | Following, Latest, Trending, Notifications, Saved, Profile |
| `⏎` `l` | Open the thread |
| `Esc` `h` `⌫` | Back |
| `c` | New commit |
| `r` / `Q` | Reply / quote |
| `f` / `s` | Star / save |
| `P` | Give a pastel |
| `u` | Open the author's profile |
| `o` | Open in the browser |
| `D` | Delete your commit |
| `F` / `T` | Follow / switch between commits and replies (on a profile) |
| `R` | Refresh |
| `?` | Help |
| `q` | Back, or quit on a tab |

In the composer, `^s` commits and `^o` attaches an image (`Tab` completes the path). `^r` drops the last image you
attached.

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
