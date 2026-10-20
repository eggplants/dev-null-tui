// Command dn is a terminal client for /dev/null (https://dev-null.sh), the social network for developers.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/eggplants/dev-null-tui/internal/api"
	"github.com/eggplants/dev-null-tui/internal/auth"
	"github.com/eggplants/dev-null-tui/internal/browser"
	"github.com/eggplants/dev-null-tui/internal/commit"
	"github.com/eggplants/dev-null-tui/internal/config"
	"github.com/eggplants/dev-null-tui/internal/tui"
)

// version is set by GoReleaser via -ldflags "-X main.version=...".
var version = ""

const usage = `Usage: dn [command]

Commands:
  (none)                              Open the TUI
  login, l [-base-url URL] [-key PATH]
                                      Approve this TUI's SSH key as a device on the web
  whoami, w                           Show the signed-in account
  commit, c [-r REF] [-i PATH]... [--] [TEXT...]
                                      Commit, signed with an SSH key on your GitHub account (no login needed)
  handle-url URL                      Forward a devnull:// callback to a waiting login (run by the desktop)

Options:
  -h, --help     Show this help and exit
  -v, --version  Show version and exit
`

func getVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "dn:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return open()
	}

	switch args[0] {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	case "-v", "--version":
		fmt.Println("dn", getVersion())
		return nil
	case "login", "l":
		return login(args[1:])
	case "whoami", "w":
		return whoami()
	case "commit", "c":
		return commitCmd(args[1:])
	case "handle-url":
		if len(args) != 2 {
			return errors.New("usage: dn handle-url URL")
		}
		return auth.Forward(args[1])
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// client returns an API client signing with the saved key.
func client() (*api.Client, error) {
	cfg, err := config.Load()
	if errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("not signed in, run `dn login`")
	}
	if err != nil {
		return nil, err
	}

	signer, err := config.LoadKey(cfg.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("reading the signing key, run `dn login` again: %w", err)
	}

	return api.New(cfg.BaseURL, signer), nil
}

func open() error {
	c, err := client()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	return tui.New(c).Run(ctx)
}

func login(args []string) error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("login", flag.ExitOnError)
	baseURL := fs.String("base-url", config.DefaultBaseURL, "the /dev/null site to sign in to")
	keyPath := fs.String("key", filepath.Join(dir, "id_ed25519"), "the private key to approve, made if missing")
	_ = fs.Parse(args)

	signer, err := config.LoadOrCreateKey(*keyPath)
	if err != nil {
		return fmt.Errorf("loading %s: %w", *keyPath, err)
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if !auth.HandlerInstalled(executable) {
		if err := auth.InstallHandler(executable); err != nil {
			fmt.Fprintln(os.Stderr, "Couldn't register the devnull:// handler:", err)
		} else {
			fmt.Println("Registered", executable, "as the handler for devnull:// links.")
		}
	}

	host, _ := os.Hostname()
	req, err := auth.NewRequest(*baseURL, signer.PublicKey(), "devnull-tui on "+host)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	callbacks, err := auth.Listen(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("Approve this key in your browser:\n\n  %s\n\n", req.AuthorizeURL)
	if err := browser.Open(req.AuthorizeURL); err != nil {
		fmt.Fprintln(os.Stderr, "Couldn't open a browser, open the link above yourself:", err)
	}
	fmt.Println("Waiting for the approval... (or paste the devnull://auth/callback URL here)")

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			if line := strings.TrimSpace(scanner.Text()); line != "" {
				_ = auth.Forward(line)
			}
		}
	}()

	var code string
	for code == "" {
		select {
		case <-ctx.Done():
			return errors.New("timed out waiting for the approval")
		case callback := <-callbacks:
			if code, err = req.Code(callback); err != nil {
				return err
			}
		}
	}

	me, err := api.New(*baseURL, signer).RedeemDevice(ctx, code, req.CodeVerifier)
	if err != nil {
		return err
	}

	cfg := &config.Config{BaseURL: *baseURL, KeyPath: *keyPath, Username: me.Data.Username}
	if err := cfg.Save(); err != nil {
		return err
	}

	fmt.Printf("Signed in as @%s. Run dn to open the TUI.\n", me.Data.Username)

	return nil
}

const commitUsage = `Usage: dn commit [-r <commit hash or link>] [-i <path>]... [--] [text...]

Commits to /dev/null like ` + "`curl -fsSL https://dev-null.sh/commit | bash -s -- 'text'`" + `, signed with an SSH key
(from your SSH agent or ~/.ssh) that is on your GitHub account. It needs no ` + "`dn login`" + `.

Options:
  -r, --reply REF    Reply to the commit with this hash, or at this link
  -i, --image PATH   Attach a JPEG, PNG or WebP image of up to 10 MB (up to 3 times)
  -h, --help         Show this help and exit
  --                 End the options, for a message that starts with one of them

Without TEXT, the message is read from stdin when it isn't a terminal, or asked for.
Set DEVNULL_URL to commit to another /dev/null.
`

// parseCommitArgs reads the options into opts, up to the first argument that isn't one or a "--", and returns the
// rest, which is the message.
func parseCommitArgs(args []string, opts *commit.Options) (rest []string, help bool, err error) {
	for len(args) > 0 {
		name, value, hasValue := strings.Cut(args[0], "=")
		switch name {
		case "-h", "--help":
			return nil, true, nil
		case "-r", "--reply", "-i", "--image":
			if !hasValue {
				if len(args) < 2 || args[1] == "" {
					return nil, false, fmt.Errorf("%s needs a value\n\n%s", name, commitUsage)
				}
				value, args = args[1], args[1:]
			}
			if name == "-i" || name == "--image" {
				opts.Images = append(opts.Images, value)
			} else if opts.ReplyTo != "" {
				return nil, false, errors.New("--reply can only be given once")
			} else {
				opts.ReplyTo = value
			}
			args = args[1:]
			continue
		case "--":
			args = args[1:]
		}

		return args, false, nil
	}

	return nil, false, nil
}

func commitCmd(args []string) error {
	opts := commit.Options{BaseURL: config.DefaultBaseURL, Passphrase: readPassphrase}
	if cfg, err := config.Load(); err == nil && cfg.BaseURL != "" {
		opts.BaseURL = cfg.BaseURL
	}
	if u := os.Getenv("DEVNULL_URL"); u != "" {
		opts.BaseURL = u
	}

	args, help, err := parseCommitArgs(args, &opts)
	if err != nil {
		return err
	}
	if help {
		fmt.Print(commitUsage)
		return nil
	}

	message, err := commitMessage(args)
	if err != nil {
		return err
	}
	opts.Message = message

	answer, err := commit.Run(context.Background(), opts)
	if err != nil {
		return err
	}
	fmt.Print(answer)

	return nil
}

// commitMessage takes the message from the arguments, else from stdin when something is piped in, else asks for it.
func commitMessage(args []string) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		return strings.TrimSpace(string(data)), err
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", errors.New(commitUsage)
	}
	defer tty.Close()

	return commit.Prompt(tty, "Commit message: ")
}

func readPassphrase(path string) ([]byte, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("%s needs a passphrase: add it to your SSH agent with ssh-add", path)
	}
	defer tty.Close()

	fmt.Fprintf(tty, "Enter passphrase for %s: ", path)
	pass, err := term.ReadPassword(int(tty.Fd()))
	fmt.Fprintln(tty)

	return pass, err
}

func whoami() error {
	c, err := client()
	if err != nil {
		return err
	}

	me, err := c.Me(context.Background())
	if err != nil {
		return err
	}

	fmt.Printf("@%s (%s), %d unread notifications\n", me.Data.Username, me.Data.Name, me.UnreadNotificationsCount)

	return nil
}
