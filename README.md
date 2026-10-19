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

A terminal client for [/dev/null](https://dev-null.sh), the social network for developers, built with [tview](https://github.com/rivo/tview).

![demo](demo.gif)

## Install

```sh
# via mise
mise use -g github:eggplants/dev-null-tui

# via go
go install github.com/eggplants/dev-null-tui/cmd/dn@latest

# via docker (sign in with `dn login` on the host first, and mount its config)
docker run --rm -it -v ~/.config/devnull-tui:/root/.config/devnull-tui:ro ghcr.io/eggplants/dev-null-tui
```

## Usage

```sh
dn login    # approve this TUI's SSH key on the web, once
dn          # open the TUI
dn whoami
dn commit [-r <commit hash or link>] [-i <path>]... [--] [text...]
dn --version
dn --help
```

### Committing from the shell

`dn commit` does what `curl -fsSL https://dev-null.sh/commit | bash -s -- 'text'` does, without bash, curl or
ssh-keygen. It needs no `dn login`: it signs the commit with an Ed25519 or RSA key from your SSH agent or `~/.ssh` that
is on your GitHub account, and /dev/null checks the signature against the keys GitHub publishes for you.

```sh
dn commit 'Hello world!'
dn commit -r 0be0130 'Agreed!'
dn commit --image shipped.png --image demo.webp 'Shipped!'
git log -1 --format=%s | dn commit
```

`-i`/`--image` takes a JPEG, PNG or WebP file of up to 10 MB, up to 3 times, and `-r`/`--reply` the hash of the
commit to reply to, or its link. Without text, the message is read from stdin,
or asked for. A key with a passphrase is unlocked through your SSH agent, or asked for on the terminal.

### Signing in

`dn` signs in the way the /dev/null mobile app does. `login` makes an ed25519 key in `~/.config/devnull-tui/`
(or approves the one you pass with `-key`) and opens `https://dev-null.sh/mobile/authorize` in your browser. Approve it
there, and the browser hands the one-time code back through a `devnull://` link, which `dn` redeems. Every
request after that is signed with the key ([SSHSIG](https://github.com/openssh/openssh-portable/blob/master/PROTOCOL.sshsig),
namespace `devnull-api`). You can revoke it under Settings → Devices.

On Linux, `login` registers itself as the desktop's handler for `devnull://` links
(`~/.local/share/applications/devnull-tui.desktop`). Run `login` again after moving the binary.

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

In the composer, `^s` commits, `^o` attaches an image (`Tab` completes the path) and `^r` removes the last one.

## Limitations

The TUI does what the [signed API](https://github.com/nunomaduro/dev-null.sh#signed-requests) does. What only the web
does is out of reach for now: search, remotes (messages and orgs), polls (voting and making them), pinning, scheduled
commits and translations. The `devnull://` handler is only registered on Linux.

The signed API only takes JSON, so a commit with images goes out the way `dn commit --image` sends it: signed with an
SSH key on your GitHub account, which has to be in your SSH agent or `~/.ssh`. A quote can't carry images.

## License

[MIT](LICENSE)
