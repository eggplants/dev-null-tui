package api

import (
	"context"
	"net/http"
	"net/url"
)

func commitPath(id string) string {
	return "/api/v1/commits/" + url.PathEscape(id)
}

// Pin pins one of the user's top-level commits to the top of their profile, in place of the one pinned before, or
// unpins it, and answers with the commit's new state.
func (c *Client) Pin(ctx context.Context, id string, pin bool) (*Commit, error) {
	method := http.MethodPut
	if !pin {
		method = http.MethodDelete
	}

	return c.commitAction(ctx, method, commitPath(id)+"/pin")
}

// RestoreWindow is how long after deleting a commit the user can still undo it, as the web's Undo can.
const RestoreWindow = 30

// Restore undoes deleting the user's commit, within RestoreWindow seconds of it.
func (c *Client) Restore(ctx context.Context, id string) (*Commit, error) {
	return c.commitAction(ctx, http.MethodPost, commitPath(id)+"/restoration")
}

// Vote votes for one of the options of a commit's poll, and answers with the commit and the poll's results.
func (c *Client) Vote(ctx context.Context, id, option string) (*Commit, error) {
	var res data[Commit]
	err := c.Do(ctx, http.MethodPost, commitPath(id)+"/poll/votes", map[string]string{"option": option}, &res)

	return &res.Data, err
}

// Stars lists who starred a commit, newest first.
func (c *Client) Stars(ctx context.Context, id, cursor string) (*Page[User], error) {
	return c.people(ctx, commitPath(id)+"/stars", cursor)
}

// Translation is a commit's body in another language. Translated is nil while it isn't ready, and Available is
// false when the commit is in that language already, or in none the service tells.
type Translation struct {
	Language       string `json:"language"`
	SourceLanguage string `json:"source_language"`
	SourceName     string `json:"source_name"`
	Available      bool   `json:"available"`
	Translated     *struct {
		BodySegments    []Segment `json:"body_segments"`
		PreviewSegments []Segment `json:"preview_segments"`
	} `json:"translated"`
}

// Translate translates a commit into a language, as a code like "ja". A failed translation is a 503 worth trying
// again.
func (c *Client) Translate(ctx context.Context, id, language string) (*Translation, error) {
	var res struct {
		Translation *Translation `json:"translation"`
	}
	if err := c.Do(ctx, http.MethodPost, commitPath(id)+"/translation", map[string]string{"language": language}, &res); err != nil {
		return nil, err
	}
	if res.Translation == nil {
		return &Translation{Language: language}, nil
	}

	return res.Translation, nil
}

// Related lists commits about the same things as a commit.
func (c *Client) Related(ctx context.Context, id string) ([]Commit, error) {
	var res data[[]Commit]
	err := c.Do(ctx, http.MethodGet, commitPath(id)+"/related", nil, &res)

	return res.Data, err
}

// Settings are what only the user sees of their settings.
type Settings struct {
	Timezone             string     `json:"timezone"`
	ShowLocalTime        bool       `json:"show_local_time"`
	TranslationLanguage  *string    `json:"translation_language"`
	AutomaticTranslation bool       `json:"automatic_translation"`
	TranslationLanguages []Language `json:"translation_languages"`
}

// Language is a language commits can be translated into.
type Language struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	NativeName string `json:"native_name"`
}

func (c *Client) Settings(ctx context.Context) (*Settings, error) {
	var res data[Settings]
	err := c.Do(ctx, http.MethodGet, "/api/v1/me/settings", nil, &res)

	return &res.Data, err
}

// Preview shows a draft as the commit it would become: the body as it would be kept, with emoji shortcodes turned
// into emoji, the segments to draw it from and whom it mentions.
func (c *Client) Preview(ctx context.Context, body string) (*Commit, error) {
	var res data[Commit]
	err := c.Do(ctx, http.MethodPost, "/api/v1/commits/preview", map[string]string{"body": body}, &res)

	return &res.Data, err
}
