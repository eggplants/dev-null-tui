package api

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Live is the stream a user is live on, until EndsAt.
type Live struct {
	Platform     string    `json:"platform"`
	URL          string    `json:"url"`
	ThumbnailURL string    `json:"thumbnail_url"`
	EndsAt       time.Time `json:"ends_at"`
}

// MaxLiveHours is the longest a user can say they are live for at once.
const MaxLiveHours = 12

// Device is a key approved to sign requests as the user, like this one.
type Device struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	AppName     string     `json:"app_name"`
	Fingerprint string     `json:"fingerprint"`
	ApprovedAt  time.Time  `json:"approved_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
}

// Devices lists the keys that may sign requests as the user, newest approval first.
func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	var res data[[]Device]
	err := c.Do(ctx, http.MethodGet, "/api/v1/me/devices", nil, &res)

	return res.Data, err
}

// RevokeDevice stops a key from signing requests as the user. Revoking the one that signs this request signs dn out.
func (c *Client) RevokeDevice(ctx context.Context, id string) error {
	return c.Do(ctx, http.MethodDelete, "/api/v1/me/devices/"+url.PathEscape(id), nil, nil)
}

// SetTranslationLanguage saves the language commits are translated into, as a code like "ja", or with nil the
// device's.
func (c *Client) SetTranslationLanguage(ctx context.Context, language *string) (*CurrentUser, error) {
	return c.saveMe(ctx, "/api/v1/me/translation-language", map[string]*string{"translation_language": language})
}

// SetAutomaticTranslation turns translating commits in other languages by themselves on or off.
func (c *Client) SetAutomaticTranslation(ctx context.Context, on bool) (*CurrentUser, error) {
	return c.saveMe(ctx, "/api/v1/me/automatic-translation", map[string]bool{"enabled": on})
}

// SetTimezone saves the timezone the web shows times in, like "Asia/Tokyo".
func (c *Client) SetTimezone(ctx context.Context, timezone string) (*CurrentUser, error) {
	return c.saveMe(ctx, "/api/v1/me/timezone", map[string]string{"timezone": timezone})
}

// GoLive marks the user live on their stream, a YouTube or Twitch link, for a number of hours.
func (c *Client) GoLive(ctx context.Context, streamURL string, hours int) (*CurrentUser, error) {
	return c.saveMe(ctx, "/api/v1/me/live", map[string]any{"url": streamURL, "hours": hours})
}

// EndLive ends the user's stream early.
func (c *Client) EndLive(ctx context.Context) (*CurrentUser, error) {
	var res CurrentUser
	err := c.Do(ctx, http.MethodDelete, "/api/v1/me/live", nil, &res)

	return &res, err
}

func (c *Client) saveMe(ctx context.Context, path string, in any) (*CurrentUser, error) {
	var res CurrentUser
	err := c.Do(ctx, http.MethodPut, path, in, &res)

	return &res, err
}

// AcceptTerms records that the user accepted the terms and privacy policy of a version, the one in force.
func (c *Client) AcceptTerms(ctx context.Context, version string) (*CurrentUser, error) {
	return c.saveMe(ctx, "/api/v1/me/terms", map[string]string{"version": version})
}
