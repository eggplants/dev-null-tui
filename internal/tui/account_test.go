package tui

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// accountAPI answers the settings only the user sees, and their devices.
type accountAPI struct {
	mu        sync.Mutex
	language  *string
	automatic bool
	timezone  string
	revoked   []string
	remotes   bool
	// termsChanged has the terms changed since the user accepted them, until they accept the version accepted.
	termsChanged bool
	accepted     string
}

func (f *accountAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	me := map[string]any{"data": map[string]any{
		"id": "u0", "handle": "me", "name": "Me", "lane": 1,
		"terms_version": "2026-11-01", "terms_accepted": f.accepted != "" || !f.termsChanged,
	}}
	var out any
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v1/me":
		out = me
	case "PUT /api/v1/me/terms":
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.accepted = in["version"]
		out = map[string]any{"data": map[string]any{"id": "u0", "handle": "me", "name": "Me", "lane": 1, "terms_version": "2026-11-01", "terms_accepted": true}}
	case "PUT /api/v1/me/remotes":
		var in map[string]bool
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.remotes = in["enabled"]
		out = map[string]any{"data": map[string]any{"id": "u0", "handle": "me", "name": "Me", "lane": 1, "remotes": map[string]any{"from": "following", "pull_receipts": true}}}
	case "GET /api/v1/me/settings":
		out = map[string]any{"data": map[string]any{
			"timezone": f.timezone, "translation_language": f.language, "automatic_translation": f.automatic,
			"translation_languages": []any{
				map[string]any{"code": "en", "name": "English", "native_name": "English"},
				map[string]any{"code": "ja", "name": "Japanese", "native_name": "日本語"},
			},
		}}
	case "PUT /api/v1/me/translation-language":
		var in map[string]*string
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.language = in["translation_language"]
		out = me
	case "PUT /api/v1/me/automatic-translation":
		var in map[string]bool
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.automatic = in["enabled"]
		out = me
	case "PUT /api/v1/me/timezone":
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.timezone = in["timezone"]
		out = me
	case "GET /api/v1/me/devices":
		var devices []any
		for _, d := range []map[string]any{
			{"id": "d1", "name": "devnull on a phone", "fingerprint": "SHA256:phone", "approved_at": time.Now().Add(-48 * time.Hour).Format(time.RFC3339)},
			{"id": "d2", "name": "an old laptop", "fingerprint": "SHA256:laptop", "approved_at": time.Now().Add(-96 * time.Hour).Format(time.RFC3339)},
		} {
			gone := false
			for _, id := range f.revoked {
				gone = gone || id == d["id"]
			}
			if !gone {
				devices = append(devices, d)
			}
		}
		out = map[string]any{"data": devices}
	case "DELETE /api/v1/me/devices/d2":
		f.revoked = append(f.revoked, "d2")
		w.WriteHeader(http.StatusNoContent)
		return
	default:
		out = map[string]any{"data": []any{}, "meta": map[string]any{"next_cursor": nil}}
	}

	_ = json.NewEncoder(w).Encode(out)
}

// openSetting opens the settings and moves down to the row labelled label.
func (h *harness) openSetting(label string, down int) {
	h.t.Helper()
	h.typeText("gs")
	h.waitFor("Pastéis mode")
	for range down {
		h.typeText("j")
	}
}

func TestPicksTheLanguageToTranslateInto(t *testing.T) {
	f := &accountAPI{}
	h := start(t, f)
	h.waitFor("Following")

	h.openSetting("your device's", 7)
	h.waitFor("picks the language t translates commits into")
	h.key(tcell.KeyEnter)
	h.waitFor("Translate commits into")
	h.typeText("japanese")
	h.key(tcell.KeyEnter)
	h.waitFor("Commits are translated into Japanese (ja) now.")

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.language == nil || *f.language != "ja" {
		t.Errorf("saved %v", f.language)
	}
}

func TestTurnsAutomaticTranslationOn(t *testing.T) {
	f := &accountAPI{}
	h := start(t, f)
	h.waitFor("Following")

	h.openSetting("Auto-translate", 8)
	h.waitFor("by themselves")
	h.key(tcell.KeyEnter)
	h.waitFor("Automatic translation is on.")

	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.automatic {
		t.Error("didn't turn it on")
	}
}

func TestSetsTheTimezone(t *testing.T) {
	f := &accountAPI{timezone: "UTC"}
	h := start(t, f)
	h.waitFor("Following")

	h.openSetting("Timezone", 9)
	h.waitFor("sets the timezone")
	h.key(tcell.KeyEnter)
	h.waitFor(" Timezone ")
	h.key(tcell.KeyCtrlU)
	h.typeText("Europe/Lisbon")
	h.key(tcell.KeyEnter)
	h.waitFor("Your timezone is Europe/Lisbon.")

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.timezone != "Europe/Lisbon" {
		t.Errorf("saved %q", f.timezone)
	}
}

func TestRevokesADevice(t *testing.T) {
	f := &accountAPI{}
	h := start(t, f)
	h.waitFor("Following")

	h.openSetting("Devices", 13)
	h.waitFor("lists them, to revoke one")
	h.key(tcell.KeyEnter)
	h.waitFor("an old laptop")

	h.typeText("j")
	h.typeText("D")
	h.waitFor("Revoke an old laptop?")
	h.key(tcell.KeyRight)
	h.key(tcell.KeyEnter)
	h.waitFor("Revoked an old laptop.")
	h.waitForGone("an old laptop ")

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.revoked) != 1 {
		t.Errorf("revoked %v", f.revoked)
	}
}

func TestTurnsRemotesOn(t *testing.T) {
	f := &accountAPI{}
	h := start(t, f)
	h.waitFor("Following")

	h.openSetting("Remotes", 11)
	h.waitFor("off: nobody can message you")
	h.key(tcell.KeyEnter)
	h.waitFor("Remotes are on.")
	h.waitFor("on: people you follow can push to you")

	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.remotes {
		t.Error("didn't turn them on")
	}
}

func TestAcceptsTheTermsWhenTheyChanged(t *testing.T) {
	f := &accountAPI{termsChanged: true}
	h := start(t, f)
	h.waitFor("The terms and privacy policy changed.")

	h.openSetting("Terms", 12)
	h.waitFor("changed: read and accept them")
	h.key(tcell.KeyEnter)
	h.waitFor("I accept them")
	h.key(tcell.KeyRight)
	h.key(tcell.KeyEnter)
	h.waitFor("You accepted the terms and privacy policy.")
	h.waitForGone("changed: read and accept them")

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.accepted != "2026-11-01" {
		t.Errorf("accepted %q", f.accepted)
	}
}
