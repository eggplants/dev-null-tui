package auth

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const desktopFile = "devnull-tui.desktop"

// InstallHandler registers `<executable> handle-url %u` as the desktop's handler for devnull:// URLs.
func InstallHandler(executable string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("registering the %s:// handler isn't supported on %s yet", scheme, runtime.GOOS)
	}

	dir, err := applicationsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	entry := strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=dn",
		"Comment=Hands /dev/null sign-in callbacks to dn",
		fmt.Sprintf("Exec=%s handle-url %%u", desktopQuote(executable)),
		"Terminal=false",
		"NoDisplay=true",
		"MimeType=x-scheme-handler/" + scheme + ";",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, desktopFile), []byte(entry), 0o644); err != nil {
		return err
	}

	if out, err := exec.Command("xdg-mime", "default", desktopFile, "x-scheme-handler/"+scheme).CombinedOutput(); err != nil {
		return fmt.Errorf("xdg-mime: %w: %s", err, out)
	}
	_ = exec.Command("update-desktop-database", dir).Run()

	return nil
}

// HandlerInstalled says whether devnull:// URLs already open this executable.
func HandlerInstalled(executable string) bool {
	if runtime.GOOS != "linux" {
		return false
	}

	out, err := exec.Command("xdg-mime", "query", "default", "x-scheme-handler/"+scheme).Output()
	if err != nil || strings.TrimSpace(string(out)) != desktopFile {
		return false
	}

	dir, err := applicationsDir()
	if err != nil {
		return false
	}
	entry, err := os.ReadFile(filepath.Join(dir, desktopFile))

	return err == nil && strings.Contains(string(entry), "Exec="+desktopQuote(executable)+" ")
}

func applicationsDir() (string, error) {
	if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
		return filepath.Join(dataHome, "applications"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".local", "share", "applications"), nil
}

// desktopQuote quotes an Exec argument as the Desktop Entry spec asks.
func desktopQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`)

	return `"` + r.Replace(s) + `"`
}
