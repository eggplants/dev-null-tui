package api

import (
	"context"
	"net/http"
	"net/url"
)

// CurrentUser is the signed-in user, as GET /api/v1/me and POST /api/v1/devices answer.
type CurrentUser struct {
	Data                     User `json:"data"`
	UnreadNotificationsCount int  `json:"unread_notifications_count"`
}

type Log string

const (
	LogFollowing Log = "following"
	LogLatest    Log = "latest"
	LogTrending  Log = "trending"
)

type data[T any] struct {
	Data T `json:"data"`
}

// Me returns the user the signing key is approved for.
func (c *Client) Me(ctx context.Context) (*CurrentUser, error) {
	var res CurrentUser
	if err := c.Do(ctx, http.MethodGet, "/api/v1/me", nil, &res); err != nil {
		return nil, err
	}

	return &res, nil
}

// RedeemDevice approves the signing key as a device with the code the authorize page handed out.
func (c *Client) RedeemDevice(ctx context.Context, code, codeVerifier string) (*CurrentUser, error) {
	var res CurrentUser
	in := map[string]string{"code": code, "code_verifier": codeVerifier}
	if err := c.Do(ctx, http.MethodPost, "/api/v1/devices", in, &res); err != nil {
		return nil, err
	}

	return &res, nil
}

func (c *Client) Log(ctx context.Context, log Log, cursor string) (*LogPage, error) {
	var res LogPage
	err := c.Do(ctx, http.MethodGet, withCursor("/api/v1/log/"+string(log), cursor, nil), nil, &res)

	return &res, err
}

func (c *Client) Commit(ctx context.Context, id string) (*Thread, error) {
	var res Thread
	err := c.Do(ctx, http.MethodGet, "/api/v1/commits/"+url.PathEscape(id), nil, &res)

	return &res, err
}

// Replies lists a commit's replies, oldest first unless newest is set, as the web's thread does.
func (c *Client) Replies(ctx context.Context, id, cursor string, newest bool) (*Page[Commit], error) {
	query := url.Values{}
	if newest {
		query.Set("newest", "1")
	}

	var res Page[Commit]
	err := c.Do(ctx, http.MethodGet, withCursor("/api/v1/commits/"+url.PathEscape(id)+"/replies", cursor, query), nil, &res)

	return &res, err
}

// Profile is a user's profile as a signed request sees it, with how the signed-in user relates to them.
type Profile struct {
	Data            User    `json:"data"`
	IsFollowing     bool    `json:"is_following"`
	FollowsYou      bool    `json:"follows_you"`
	IsMuting        bool    `json:"is_muting"`
	PasteisReceived int     `json:"pasteis_received"`
	PinnedCommit    *Commit `json:"pinned_commit"`
}

func (c *Client) Profile(ctx context.Context, username string) (*Profile, error) {
	var res Profile
	err := c.Do(ctx, http.MethodGet, "/api/v1/users/"+url.PathEscape(username), nil, &res)

	return &res, err
}

// UserCommits lists a user's top-level commits, or their replies when replies is set.
func (c *Client) UserCommits(ctx context.Context, username, cursor string, replies bool) (*Page[Commit], error) {
	query := url.Values{}
	if replies {
		query.Set("replies", "1")
	}

	var res Page[Commit]
	err := c.Do(ctx, http.MethodGet, withCursor("/api/v1/users/"+url.PathEscape(username)+"/commits", cursor, query), nil, &res)

	return &res, err
}

func (c *Client) Saved(ctx context.Context, cursor string) (*Page[Commit], error) {
	var res Page[Commit]
	err := c.Do(ctx, http.MethodGet, withCursor("/api/v1/saved", cursor, nil), nil, &res)

	return &res, err
}

// Notifications lists the notification groups. Listing them marks every notification as read.
func (c *Client) Notifications(ctx context.Context, cursor string) (*Page[Notification], error) {
	var res Page[Notification]
	err := c.Do(ctx, http.MethodGet, withCursor("/api/v1/notifications", cursor, nil), nil, &res)

	return &res, err
}

// NewCommit is a commit to make. ParentID makes it a reply and QuotedCommitID a quote; only one of them may be set.
type NewCommit struct {
	Body           string `json:"body"`
	ParentID       string `json:"parent_id,omitempty"`
	QuotedCommitID string `json:"quoted_commit_id,omitempty"`
}

func (c *Client) CreateCommit(ctx context.Context, commit NewCommit) (*Commit, error) {
	var res data[Commit]
	err := c.Do(ctx, http.MethodPost, "/api/v1/commits", commit, &res)

	return &res.Data, err
}

func (c *Client) DeleteCommit(ctx context.Context, id string) error {
	return c.Do(ctx, http.MethodDelete, "/api/v1/commits/"+url.PathEscape(id), nil, nil)
}

// Star stars or unstars a commit, and answers with the commit's new state.
func (c *Client) Star(ctx context.Context, id string, star bool) (*Commit, error) {
	return c.commitAction(ctx, toggle(star), "/api/v1/commits/"+url.PathEscape(id)+"/star")
}

// Save saves or unsaves a commit, and answers with the commit's new state.
func (c *Client) Save(ctx context.Context, id string, save bool) (*Commit, error) {
	method := http.MethodPut
	if !save {
		method = http.MethodDelete
	}

	return c.commitAction(ctx, method, "/api/v1/commits/"+url.PathEscape(id)+"/bookmark")
}

// GivePastel gives a commit one of the user's daily pastéis, and answers with the commit's new state.
func (c *Client) GivePastel(ctx context.Context, id string) (*Commit, error) {
	return c.commitAction(ctx, http.MethodPost, "/api/v1/commits/"+url.PathEscape(id)+"/pasteis")
}

func (c *Client) Follow(ctx context.Context, username string, follow bool) error {
	return c.Do(ctx, toggle(follow), "/api/v1/users/"+url.PathEscape(username)+"/follow", nil, nil)
}

func (c *Client) commitAction(ctx context.Context, method, path string) (*Commit, error) {
	var res data[Commit]
	err := c.Do(ctx, method, path, nil, &res)

	return &res.Data, err
}

func toggle(on bool) string {
	if on {
		return http.MethodPost
	}

	return http.MethodDelete
}

func withCursor(path, cursor string, query url.Values) string {
	if cursor != "" {
		if query == nil {
			query = url.Values{}
		}
		query.Set("cursor", cursor)
	}
	if len(query) == 0 {
		return path
	}

	return path + "?" + query.Encode()
}
