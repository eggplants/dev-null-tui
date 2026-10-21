package api

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// CurrentUser is the signed-in user, as GET /api/v1/me and POST /api/v1/devices answer.
type CurrentUser struct {
	Data                     CurrentUserData `json:"data"`
	UnreadNotificationsCount int             `json:"unread_notifications_count"`
}

// CurrentUserData is the signed-in user with what only they see of themselves.
type CurrentUserData struct {
	User
	PasteisMode      bool       `json:"pasteis_mode"`
	PasteisRemaining int        `json:"pasteis_remaining"`
	PasteisResetsAt  *time.Time `json:"pasteis_resets_at"`
	PasteisReceived  int        `json:"pasteis_received"`
	// PreferredLane is the lane the user picked, nil when they pick none or a custom color, and AutomaticLane the
	// one they get then.
	PreferredLane *int       `json:"preferred_lane"`
	AutomaticLane int        `json:"automatic_lane"`
	SyncedAt      *time.Time `json:"github_synced_at"`
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

// NewCommitsCount counts the commits in a log newer than since, leaving out the user's own, as the web's "new commits"
// button does. It is always 0 on a ranked log.
func (c *Client) NewCommitsCount(ctx context.Context, log Log, since string) (int, error) {
	var res LogPage
	err := c.Do(ctx, http.MethodGet, withCursor("/api/v1/log/"+string(log), "", url.Values{"since": {since}}), nil, &res)

	return res.NewCommitsCount, err
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
	Data            User          `json:"data"`
	IsFollowing     bool          `json:"is_following"`
	FollowsYou      bool          `json:"follows_you"`
	IsMuting        bool          `json:"is_muting"`
	PasteisReceived int           `json:"pasteis_received"`
	PinnedCommit    *Commit       `json:"pinned_commit"`
	Activity        *Activity     `json:"activity"`
	Streaks         *Streaks      `json:"streaks"`
	Achievements    *Achievements `json:"achievements"`
}

// Activity is how many commits a user made in each hour (on the "day" scale) or day of a window that reaches back
// to their first commit: a "day", "week", "month" or "year".
type Activity struct {
	Scale   string           `json:"scale"`
	Buckets []ActivityBucket `json:"buckets"`
}

// ActivityBucket's Date is "2006-01-02", or an hour in RFC 3339 on the day scale.
type ActivityBucket struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// Streaks count days in a row with a commit. Current is 0 unless the last commit was today or yesterday.
type Streaks struct {
	Longest int `json:"longest"`
	Current int `json:"current"`
}

// Achievements are a user's Unix rank, earned with XP, and their badges.
type Achievements struct {
	Level         int     `json:"level"`
	Rank          string  `json:"rank"`
	XP            int     `json:"xp"`
	NextLevelXP   *int    `json:"next_level_xp"`
	LevelProgress int     `json:"level_progress"`
	Badges        []Badge `json:"badges"`
}

type Badge struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Unlocked    bool   `json:"unlocked"`
	Progress    int    `json:"progress"`
	Target      int    `json:"target"`
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

// Starred lists the commits the user starred, last starred first.
func (c *Client) Starred(ctx context.Context, cursor string) (*Page[Commit], error) {
	var res Page[Commit]
	err := c.Do(ctx, http.MethodGet, withCursor("/api/v1/starred", cursor, nil), nil, &res)

	return &res, err
}

// NotificationTypes are the kinds of notifications the list can be filtered to, as the web's filter names them.
var NotificationTypes = []string{"replies", "mentions", "quotes", "stars", "follows", "moderation"}

// Notifications lists the notification groups of a type, or of every type when it is empty or "all". Listing them
// marks every notification as read, whatever the type.
func (c *Client) Notifications(ctx context.Context, cursor, typ string) (*Page[Notification], error) {
	if typ == "" {
		typ = "all"
	}

	var res Page[Notification]
	err := c.Do(ctx, http.MethodGet, withCursor("/api/v1/notifications", cursor, url.Values{"type": {typ}}), nil, &res)

	return &res, err
}

// NotificationGroup lists the notifications a group stands for, newest first, without marking any as read.
func (c *Client) NotificationGroup(ctx context.Context, id, cursor, typ string) (*Page[Notification], error) {
	query := url.Values{}
	if typ != "" && typ != "all" {
		query.Set("type", typ)
	}

	var res Page[Notification]
	err := c.Do(ctx, http.MethodGet, withCursor("/api/v1/notifications/"+url.PathEscape(id)+"/group", cursor, query), nil, &res)

	return &res, err
}

// Followers lists who follows a user, and Following whom they follow, each person with how the signed-in user and
// they follow each other.
func (c *Client) Followers(ctx context.Context, username, cursor string) (*Page[User], error) {
	return c.people(ctx, "/api/v1/users/"+url.PathEscape(username)+"/followers", cursor)
}

func (c *Client) Following(ctx context.Context, username, cursor string) (*Page[User], error) {
	return c.people(ctx, "/api/v1/users/"+url.PathEscape(username)+"/following", cursor)
}

// Muted lists the people the user mutes, and Blocked those they block, last first.
func (c *Client) Muted(ctx context.Context, cursor string) (*Page[User], error) {
	return c.people(ctx, "/api/v1/me/muted", cursor)
}

func (c *Client) Blocked(ctx context.Context, cursor string) (*Page[User], error) {
	return c.people(ctx, "/api/v1/me/blocked", cursor)
}

func (c *Client) people(ctx context.Context, path, cursor string) (*Page[User], error) {
	var res Page[User]
	err := c.Do(ctx, http.MethodGet, withCursor(path, cursor, nil), nil, &res)

	return &res, err
}

// Mute hides a user's commits from the user's logs without them knowing, or shows them again.
func (c *Client) Mute(ctx context.Context, username string, mute bool) error {
	return c.Do(ctx, toggle(mute), "/api/v1/users/"+url.PathEscape(username)+"/mute", nil, nil)
}

// Block keeps a user from seeing and answering the user's commits, unfollowing both ways, or lets them again.
func (c *Client) Block(ctx context.Context, username string, block bool) error {
	return c.Do(ctx, toggle(block), "/api/v1/users/"+url.PathEscape(username)+"/block", nil, nil)
}

// ReportReasons are the reasons a commit can be reported for, with the words the web gives them.
var ReportReasons = []struct{ ID, Label string }{
	{"spam", "Spam or misleading links"},
	{"harassment", "Harassment or hate"},
	{"unsafe", "Harmful or private information"},
	{"other", "Something else"},
}

// Report reports a commit to the moderators.
func (c *Client) Report(ctx context.Context, id, reason, details string) error {
	in := map[string]string{"reason": reason}
	if details != "" {
		in["details"] = details
	}

	return c.Do(ctx, http.MethodPost, "/api/v1/commits/"+url.PathEscape(id)+"/reports", in, nil)
}

// SetLane picks one of the six lanes as the user's color, or with lane nil the automatic one.
func (c *Client) SetLane(ctx context.Context, lane *int) (*CurrentUser, error) {
	var res CurrentUser
	err := c.Do(ctx, http.MethodPut, "/api/v1/me/lane", map[string]*int{"lane": lane}, &res)

	return &res, err
}

// SetColor picks a custom "#rrggbb" as the user's color.
func (c *Client) SetColor(ctx context.Context, color string) (*CurrentUser, error) {
	var res CurrentUser
	err := c.Do(ctx, http.MethodPut, "/api/v1/me/lane", map[string]string{"color": color}, &res)

	return &res, err
}

// SetPasteisMode turns pastéis mode on or off.
func (c *Client) SetPasteisMode(ctx context.Context, on bool) (*CurrentUser, error) {
	var res CurrentUser
	err := c.Do(ctx, http.MethodPut, "/api/v1/me/pasteis-mode", map[string]bool{"enabled": on}, &res)

	return &res, err
}

// SyncGitHub takes the user's name, bio and the rest of their profile from GitHub again.
func (c *Client) SyncGitHub(ctx context.Context) (*CurrentUser, error) {
	var res CurrentUser
	err := c.Do(ctx, http.MethodPut, "/api/v1/me/github", nil, &res)

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

// Pastel is the answer to giving a pastel: the commit's new state and how many the user has left today.
type Pastel struct {
	Commit           Commit `json:"data"`
	PasteisRemaining int    `json:"pasteis_remaining"`
}

// GivePastel gives a commit one of the user's daily pastéis.
func (c *Client) GivePastel(ctx context.Context, id string) (*Pastel, error) {
	var res Pastel
	err := c.Do(ctx, http.MethodPost, "/api/v1/commits/"+url.PathEscape(id)+"/pasteis", nil, &res)

	return &res, err
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
