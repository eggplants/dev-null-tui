package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Conversation kinds: a remote between two people, or an org of up to MaxOrgMembers.
const (
	ConversationDirect = "direct"
	ConversationOrg    = "org"
)

// MaxOrgMembers is the most people an org holds, its maintainer included.
const MaxOrgMembers = 100

// MaxMessageLength is the most characters a message holds.
const MaxMessageLength = 2000

// Inbox tabs, as the web's Remotes page has them.
const (
	InboxOpen    = "open"
	InboxFetched = "fetched"
	InboxStashed = "stashed"
)

// Conversation is a remote or an org as the inbox lists it.
type Conversation struct {
	ID string `json:"id"`
	// Kind is ConversationDirect or ConversationOrg.
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Partner *User  `json:"partner"`
	Members []struct {
		User User   `json:"user"`
		Role string `json:"role"`
	} `json:"members"`
	LastMessage   *Message   `json:"last_message"`
	LastMessageAt *time.Time `json:"last_message_at"`
	// Ahead is how many messages came in that the user hasn't read.
	Ahead      int        `json:"ahead"`
	IsArchived bool       `json:"is_archived"`
	PinnedAt   *time.Time `json:"pinned_at"`
	MutedUntil *time.Time `json:"muted_until_at"`
}

// Message is a message in a remote or an org. An event, like someone joining an org, has Event set and no body.
type Message struct {
	ID           int64     `json:"id"`
	UserID       string    `json:"user_id"`
	ClientID     string    `json:"client_id"`
	Body         string    `json:"body"`
	BodySegments []Segment `json:"body_segments"`
	Mentions     []Mention `json:"mentions"`
	ReplyToID    *int64    `json:"reply_to_id"`
	ReplyTo      *struct {
		ID          int64  `json:"id"`
		UserID      string `json:"user_id"`
		Body        string `json:"body"`
		ImagesCount int    `json:"images_count"`
		HasVoice    bool   `json:"has_voice"`
	} `json:"reply_to"`
	LinkPreview *LinkPreview `json:"link_preview"`
	Images      []Image      `json:"images"`
	Voice       *struct {
		ID         string `json:"id"`
		DurationMS int    `json:"duration_ms"`
		URL        string `json:"url"`
	} `json:"voice"`
	Reactions []Reaction `json:"reactions"`
	Event     *string    `json:"event"`
	SubjectID *string    `json:"subject_id"`
	User      *User      `json:"user"`
	Subject   *User      `json:"subject"`
	PinnedAt  *time.Time `json:"pinned_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// Reaction is an emoji someone left on a message.
type Reaction struct {
	UserID string `json:"user_id"`
	Emoji  string `json:"emoji"`
}

// Inbox is a page of the inbox, with how many conversations have unread messages and how many requests wait.
type Inbox struct {
	Data          []Conversation `json:"data"`
	HasMore       bool           `json:"has_more"`
	UnreadCount   int            `json:"unread_count"`
	RequestsCount int            `json:"requests_count"`
}

// Inbox lists a tab of the inbox, pinned first then newest first, matching q if it isn't empty, and continuing before a
// conversation's id.
func (c *Client) Inbox(ctx context.Context, tab, q, before string) (*Inbox, error) {
	query := url.Values{"tab": {tab}}
	if q != "" {
		query.Set("q", q)
	}
	if before != "" {
		query.Set("before", before)
	}

	var res Inbox
	err := c.Do(ctx, http.MethodGet, "/api/v1/remotes?"+query.Encode(), nil, &res)

	return &res, err
}

// RemoteThread is a page of a remote's or an org's messages, oldest first.
type RemoteThread struct {
	Messages []Message `json:"messages"`
	HasOlder bool      `json:"has_older"`
	// ReactionsVersion is how far the reactions have changed; asking with it gets only the changes after.
	ReactionsVersion int64 `json:"reactions_version"`
	ReactionChanges  []struct {
		ID        int64      `json:"id"`
		Reactions []Reaction `json:"reactions"`
	} `json:"reaction_changes"`
	ReadThroughID int64      `json:"read_through_id"`
	IsArchived    bool       `json:"is_archived"`
	PinnedAt      *time.Time `json:"pinned_at"`
	MutedUntil    *time.Time `json:"muted_until_at"`
	// Access says whether the user can write: "open", "request", "waiting", "restricted" or "closed".
	Access string `json:"access"`

	// Partner and PartnerReadThroughID are a remote's.
	Partner              *User   `json:"partner"`
	PartnerReadThroughID int64   `json:"partner_read_through_id"`
	PartnerTyping        bool    `json:"partner_typing"`
	PartnerReadAt        *string `json:"partner_read_at"`

	// Org, Members, Role, Typing and Pinned are an org's.
	Org *struct {
		ID   string  `json:"id"`
		Name *string `json:"name"`
	} `json:"org"`
	Members []OrgMember `json:"members"`
	Role    *string     `json:"role"`
	Typing  []string    `json:"typing"`
	Pinned  []Message   `json:"pinned"`
}

// OrgMember is someone in an org: a "maintainer" or a "contributor".
type OrgMember struct {
	User          User   `json:"user"`
	Role          string `json:"role"`
	ReadThroughID int64  `json:"read_through_id"`
}

// ThreadPage says which page of a thread to ask for: the latest with neither set, the one before a message, or
// everything after one with the reactions changed since a version.
type ThreadPage struct {
	Before, After    int64
	ReactionsAfter   int64
	WithReactionsLog bool
}

func (p ThreadPage) query() string {
	query := url.Values{}
	if p.Before > 0 {
		query.Set("before", strconv.FormatInt(p.Before, 10))
	}
	if p.After > 0 {
		query.Set("after", strconv.FormatInt(p.After, 10))
	}
	if p.WithReactionsLog {
		query.Set("reactions_after", strconv.FormatInt(p.ReactionsAfter, 10))
	}
	if len(query) == 0 {
		return ""
	}

	return "?" + query.Encode()
}

// Remote names a conversation: a remote with someone by their handle, or an org by its id.
type Remote struct {
	Handle string
	OrgID  string
}

func (r Remote) path() string {
	if r.OrgID != "" {
		return "/api/v1/remotes/orgs/" + url.PathEscape(r.OrgID)
	}

	return "/api/v1/remotes/@" + url.PathEscape(r.Handle)
}

// Messages reads a page of a remote's or an org's messages.
func (c *Client) Messages(ctx context.Context, r Remote, page ThreadPage) (*RemoteThread, error) {
	var res data[RemoteThread]
	err := c.Do(ctx, http.MethodGet, r.path()+"/messages"+page.query(), nil, &res)

	return &res.Data, err
}

// NewMessage is a message to push. ClientID, a UUID, makes sending it again a retry rather than a second message.
type NewMessage struct {
	Body              string   `json:"body,omitempty"`
	ClientID          string   `json:"client_id"`
	ReplyToID         *int64   `json:"reply_to_id,omitempty"`
	ImageDescriptions []string `json:"image_descriptions,omitempty"`
}

// Push sends a message, starting a remote if it is the first, with images if there are any.
func (c *Client) Push(ctx context.Context, r Remote, m NewMessage, images []File) (*Message, error) {
	var res data[Message]
	var err error
	if len(images) == 0 {
		err = c.Do(ctx, http.MethodPost, r.path()+"/messages", m, &res)
	} else {
		err = c.DoFiles(ctx, http.MethodPost, r.path()+"/messages", m, "images", images, &res)
	}

	return &res.Data, err
}

// MarkRead pulls a conversation through a message, as reading it on the web does. It only ever moves forward.
func (c *Client) MarkRead(ctx context.Context, r Remote, through int64) error {
	return c.Do(ctx, http.MethodPut, r.path()+"/read", map[string]int64{"through": through}, nil)
}

// Typing says the user is typing for a few seconds.
func (c *Client) Typing(ctx context.Context, r Remote) error {
	return c.Do(ctx, http.MethodPut, r.path()+"/typing", nil, nil)
}

// Ping pings the partner, or everyone in an org.
func (c *Client) Ping(ctx context.Context, r Remote) error {
	return c.Do(ctx, http.MethodPost, r.path()+"/ping", nil, nil)
}

// Stash stashes a conversation for the user only, or pops it back into Open. It pops back by itself when someone
// writes.
func (c *Client) Stash(ctx context.Context, r Remote, stash bool) error {
	return c.Do(ctx, toggle(stash), r.path()+"/archive", nil, nil)
}

// MuteDurations are how long a conversation can be muted for, with the words the web gives them; "off" unmutes it.
var MuteDurations = []struct{ ID, Label string }{
	{"hour", "For an hour"}, {"tomorrow", "Until tomorrow"}, {"week", "For a week"}, {"forever", "Until I unmute it"},
}

// MuteConversation mutes a conversation for the user only, for one of MuteDurations, or unmutes it with "off".
func (c *Client) MuteConversation(ctx context.Context, r Remote, duration string) error {
	return c.Do(ctx, http.MethodPut, r.path()+"/mute", map[string]string{"duration": duration}, nil)
}

// PinConversation keeps a conversation on top of the inbox, or lets it go.
func (c *Client) PinConversation(ctx context.Context, r Remote, pinned bool) error {
	return c.Do(ctx, http.MethodPut, r.path()+"/pin", map[string]bool{"pinned": pinned}, nil)
}

// Merge takes a fetched request into the open conversations, so the user can answer it.
func (c *Client) Merge(ctx context.Context, handle string) error {
	return c.Do(ctx, http.MethodPost, Remote{Handle: handle}.path()+"/merge", nil, nil)
}

// Drop drops a fetched request for good, without telling the sender.
func (c *Client) Drop(ctx context.Context, handle string) error {
	return c.Do(ctx, http.MethodPost, Remote{Handle: handle}.path()+"/drop", nil, nil)
}

func messagePath(id int64) string {
	return "/api/v1/remotes/messages/" + strconv.FormatInt(id, 10)
}

// React leaves an emoji on a message, or takes it off, and answers with the message's reactions.
func (c *Client) React(ctx context.Context, message int64, emoji string, react bool) ([]Reaction, error) {
	var res struct {
		Reactions []Reaction `json:"reactions"`
	}
	err := c.Do(ctx, toggle(react), messagePath(message)+"/reactions", map[string]string{"emoji": emoji}, &res)

	return res.Reactions, err
}

// ReactionUsage are the emoji the user reacts with, most used first.
func (c *Client) ReactionUsage(ctx context.Context) ([]string, error) {
	var res struct {
		Usage []string `json:"usage"`
	}
	err := c.Do(ctx, http.MethodGet, "/api/v1/remotes/reactions", nil, &res)

	return res.Usage, err
}

// PinMessage pins a message to the top of its conversation, or unpins it.
func (c *Client) PinMessage(ctx context.Context, message int64, pin bool) error {
	method := http.MethodPut
	if !pin {
		method = http.MethodDelete
	}

	return c.Do(ctx, method, messagePath(message)+"/pin", nil, nil)
}

// Candidate is someone the user can message or add to an org, or Reason why not.
type Candidate struct {
	User   User    `json:"user"`
	Reason *string `json:"reason"`
}

// Candidates finds the people matching q to start a remote with ("remote"), or to add to an org ("org", with its id).
func (c *Client) Candidates(ctx context.Context, q, purpose, org string) ([]Candidate, error) {
	query := url.Values{"q": {q}, "for": {purpose}}
	if org != "" {
		query.Set("org", org)
	}

	var res struct {
		Candidates []Candidate `json:"candidates"`
	}
	err := c.Do(ctx, http.MethodGet, "/api/v1/remotes/candidates?"+query.Encode(), nil, &res)

	return res.Candidates, err
}

// CreateOrg starts an org the user maintains, with the people named by their handles, and answers with its id.
func (c *Client) CreateOrg(ctx context.Context, name string, members []string) (string, error) {
	var res struct {
		Org struct {
			ID string `json:"id"`
		} `json:"org"`
	}
	err := c.Do(ctx, http.MethodPost, "/api/v1/remotes/orgs", map[string]any{"name": name, "members": members}, &res)

	return res.Org.ID, err
}

// RenameOrg names an org, or takes its name away with "".
func (c *Client) RenameOrg(ctx context.Context, org, name string) error {
	return c.Do(ctx, http.MethodPut, Remote{OrgID: org}.path(), map[string]string{"name": name}, nil)
}

// AddOrgMembers adds people to an org, sharing the history before they joined if shareHistory is set.
func (c *Client) AddOrgMembers(ctx context.Context, org string, members []string, shareHistory bool) error {
	return c.Do(ctx, http.MethodPost, Remote{OrgID: org}.path()+"/members", map[string]any{"members": members, "share_history": shareHistory}, nil)
}

// SetOrgRole makes someone in an org a "maintainer" or a "contributor".
func (c *Client) SetOrgRole(ctx context.Context, org, handle, role string) error {
	return c.Do(ctx, http.MethodPut, Remote{OrgID: org}.path()+"/members/"+url.PathEscape(handle), map[string]string{"role": role}, nil)
}

// RemoveOrgMember takes someone out of an org; the user's own handle leaves it.
func (c *Client) RemoveOrgMember(ctx context.Context, org, handle string) error {
	return c.Do(ctx, http.MethodDelete, Remote{OrgID: org}.path()+"/members/"+url.PathEscape(handle), nil, nil)
}

// RemotesSettings are who can push to the user and whether they share how far they have read.
type RemotesSettings struct {
	From         string `json:"from"`
	PullReceipts bool   `json:"pull_receipts"`
	Requests     bool   `json:"requests"`
	Everyone     bool   `json:"everyone"`
	UnreadCount  int    `json:"unread_count"`
}

// SetRemotes turns Remotes on or off. While they are off, nobody can message the user.
func (c *Client) SetRemotes(ctx context.Context, on bool) (*CurrentUser, error) {
	return c.saveMe(ctx, "/api/v1/me/remotes", map[string]bool{"enabled": on})
}

// SetRemotePreferences saves who can push to the user, "following" or "everyone", and whether they share pulls.
func (c *Client) SetRemotePreferences(ctx context.Context, from string, pullReceipts bool) (*CurrentUser, error) {
	return c.saveMe(ctx, "/api/v1/me/remote-preferences", map[string]any{"from": from, "pull_receipts": pullReceipts})
}
