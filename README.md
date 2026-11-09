# `dn`: A terminal client for [dev-null.sh](https://dev-null.sh)

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

<img width="1393" height="827" alt="image" src="https://github.com/user-attachments/assets/e6290f4d-56ab-4120-a23d-0d80acd63ed9" />

https://github.com/user-attachments/assets/f44e8bf3-aa9e-4f31-9abc-a0167c4f7756

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
dn l

# check
dn whoami
dn w

# open the TUI
dn
```

### Committing from the shell

`dn commit` / `dn c` signs and pushes the commit with an Ed25519 or RSA key from your SSH agent or `~/.ssh`.

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

To revoke it, go to `g s` → Devices, or [`Settings → Devices`](https://dev-null.sh/settings/devices) on the web.

On Linux, `login` registers `dn` as the desktop's handler for `devnull://` links. If you move the binary, run `login` again.

### Keys

The keys are the web's where it has them. The status bar shows the ones that work on what is selected, and `?` all of
them.

| Key | Action |
| --- | --- |
| `j` `k` `↓` `↑` | Next / previous (scrolls through long commits) |
| `gg` `G` | First / last |
| `^d` `^u` | Page down / up |
| `1`–`7` `Tab` | Following, Latest, Trending, Notifications, Starred, Profile, Remotes |
| `g` then `l` `e` `p` `s` | Your log (Following), Trending, Profile, Settings, as on the web |
| `g` then `a` `n` `b` `m` | Latest, Notifications, Starred, Remotes |
| `g` then `q` | Your scheduled commits: `⏎` commits one now, `D` cancels it |
| `g` then `w` | Who to follow: `f` follows, `x` says not interested, `R` suggests others |
| `/` `^k` | Search commits and people, or go to an `@username`, a profile's or commit's link, or a hash on screen |
| `⏎` `l` (or click it again) | Open the thread; on the open commit, reply |
| `Esc` `h` `⌫` | Back; `Esc` at a tab's top quits |
| `c` | New commit |
| `r` / `q` | Reply / quote |
| `o` | Show / hide the replies under a commit |
| `s` | Star |
| `S` | List who starred it |
| `P` | Give a pastel (in pastéis mode) |
| `u` | Open the author's profile |
| `y` | Copy the link (over SSH too, with OSC 52) |
| `w` | Open in the browser |
| `i` | View the images |
| `v` | Vote on its poll |
| `t` | Translate it into your language (set on the web), and back |
| `p` | Pin your commit to your profile, or unpin it |
| `D` | Delete your commit |
| `U` | Undo the delete, for 30 seconds |
| `!` | Report a commit |
| `F` / `M` / `B` | Follow / mute / block (on a profile; organizations can be followed) |
| `T` | Next tab of a profile (commits, replies, followers, following, orgs), an organization (commits, mentions) or a search (commits, people) |
| `f` / `e` | Filter notifications by kind / list everyone in a group |
| `R` | Refresh |
| `?` | Help |
| `Q` `^c` | Quit |

In Remotes:

| Key | Action |
| --- | --- |
| `n` / `N` | Push to someone new / start an org |
| `⏎` | Open the remote or org |
| `S` / `m` / `p` | Stash / mute / pin it in the inbox (`a` / `x` accept or drop a request) |
| `c` `⏎` / `r` | Push a message / reply to the selected one |
| `e` / `p` | React to the message / pin it |
| `P` | Ping them |
| `M` | The org's members: `+` adds people, `m` makes one a maintainer or a contributor, `D` takes them out, `E` renames the org, `L` leaves it |

In the composer:

| Key | Action |
| --- | --- |
| `^s` `^⏎` | Commit (`^⏎` where the terminal tells it from `⏎`) |
| `Esc` | Close; a new commit's text is kept as a draft for the next `c` |
| `^e` | Write it in `$VISUAL` or `$EDITOR` |
| `^p` | Preview |
| `Tab` | Complete the `@mention` being typed, as the web suggests it, or a `#number` into the link of that /dev/null issue |
| `^o` | Attach an image (`Tab` completes the path); pasting or dropping an image file attaches it too |
| `^v` | Attach the image on the clipboard |
| `^r` | Drop the last image |
| `^l` | Ask a poll with a new commit |
| `^t` | Commit it later: `+2h`, `18:30`, `tomorrow 9:00` or `2026-11-12 09:00` |

### Settings

`g s` in the TUI picks your color, turns pastéis mode on or off, picks the language `t` translates into and whether
commits are translated by themselves, sets your timezone, says you're live on your stream, takes your profile from
GitHub again, lists who you mute and block, and lists the devices that sign in as you, to revoke one. The theme follows the terminal's background unless you pick one.

It also switches the settings below, which take effect at once (`graphics` from the next start) and are kept in
`settings.json` next to the config. You can edit that file too; environment variables override it:

| Setting | Environment | Default | |
| --- | --- | --- | --- |
| `theme` | `DN_THEME` | `auto` | `auto`, `dark` or `light` |
| `graphics` | `DN_GRAPHICS` | `auto` | `auto`, `kitty`, `sixel` or `none` |
| `poll_interval` | `DN_POLL_INTERVAL` | `30s` | How often new commits and notifications are checked for, 10s to 10m |
| `mouse` | `DN_MOUSE` | `true` | |
| `inline_images` | `DN_INLINE_IMAGES` | `true` | Show commits' images in the lists, in pixels with kitty graphics or sixel and else in half blocks, and avatars, which are initials without pixels |

In tmux, images reach a kitty, Ghostty or WezTerm outside it once tmux lets them through with
`set -g allow-passthrough on`.

## Limitations

The TUI can do whatever the [signed API](https://github.com/nunomaduro/dev-null.sh#signed-requests) can.

Features that only the web and the apps have are out of reach for now:
- calls
- recording voice notes (`w` plays one in the browser)

And the `devnull://` handler only gets registered on Linux.

## License

[MIT](LICENSE)
