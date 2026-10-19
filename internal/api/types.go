package api

import (
	"encoding/json"
	"time"
)

type User struct {
	ID             string    `json:"id"`
	Username       string    `json:"username"`
	Name           string    `json:"name"`
	Bio            string    `json:"bio"`
	Company        string    `json:"company"`
	Location       string    `json:"location"`
	Website        string    `json:"website"`
	HTMLURL        string    `json:"html_url"`
	CreatedAt      time.Time `json:"created_at"`
	CommitsCount   *int      `json:"commits_count"`
	FollowersCount *int      `json:"followers_count"`
	FollowingCount *int      `json:"following_count"`
	// Lane is the user's color: a lane number from 0 to 5, or a custom "#rrggbb".
	Lane json.RawMessage `json:"lane"`
}

// Segment is a piece of a commit's body: "text", inline "code", or a fenced code "block" highlighted as HTML.
type Segment struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Code     string `json:"code"`
	Language string `json:"language"`
	HTML     string `json:"html"`
}

type Image struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type LinkPreview struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	SiteName    string `json:"site_name"`
}

type PollOption struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	VotesCount *int   `json:"votes_count"`
	Voted      bool   `json:"voted"`
	Percentage *int   `json:"percentage"`
}

type Poll struct {
	ID         string       `json:"id"`
	ClosesAt   *time.Time   `json:"closes_at"`
	IsClosed   bool         `json:"is_closed"`
	HasVoted   bool         `json:"has_voted"`
	TotalVotes *int         `json:"total_votes"`
	Options    []PollOption `json:"options"`
}

type Commit struct {
	ID              string        `json:"id"`
	ShortHash       string        `json:"short_hash"`
	Body            string        `json:"body"`
	BodySegments    []Segment     `json:"body_segments"`
	PreviewSegments []Segment     `json:"preview_segments"`
	ParentID        *string       `json:"parent_id"`
	HTMLURL         string        `json:"html_url"`
	CreatedAt       time.Time     `json:"created_at"`
	DeletedAt       *time.Time    `json:"deleted_at"`
	Status          string        `json:"status"`
	StarsCount      int           `json:"stars_count"`
	RepliesCount    int           `json:"replies_count"`
	PasteisCount    int           `json:"pasteis_count"`
	Starred         bool          `json:"starred"`
	IsSaved         bool          `json:"is_saved"`
	IsPinned        bool          `json:"is_pinned"`
	Author          *User         `json:"author"`
	Images          []Image       `json:"images"`
	LinkPreview     *LinkPreview  `json:"link_preview"`
	Poll            *Poll         `json:"poll"`
	QuotedCommitID  *string       `json:"quoted_commit_id"`
	QuotedCommit    *Commit       `json:"quoted_commit"`
	ReplyPreview    *Page[Commit] `json:"reply_preview"`
}

type Notification struct {
	ID          string     `json:"id"`
	Type        string     `json:"type"`
	SubjectID   string     `json:"subject_id"`
	Reason      *string    `json:"reason"`
	ReadAt      *time.Time `json:"read_at"`
	CreatedAt   time.Time  `json:"created_at"`
	Actor       *User      `json:"actor"`
	Actors      []User     `json:"actors"`
	GroupID     string     `json:"group_id"`
	EventCount  int        `json:"event_count"`
	ActorsCount int        `json:"actors_count"`
	UnreadCount int        `json:"unread_count"`
	Commit      *Commit    `json:"commit"`
}

// Page is one page of a cursor-paginated list. NextCursor is empty on the last page.
type Page[T any] struct {
	Data       []T
	NextCursor string
}

func (p *Page[T]) UnmarshalJSON(data []byte) error {
	var raw struct {
		Data       []T     `json:"data"`
		NextCursor *string `json:"next_cursor"`
		Meta       struct {
			NextCursor *string `json:"next_cursor"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	p.Data = raw.Data
	switch {
	case raw.Meta.NextCursor != nil:
		p.NextCursor = *raw.Meta.NextCursor
	case raw.NextCursor != nil:
		p.NextCursor = *raw.NextCursor
	default:
		p.NextCursor = ""
	}

	return nil
}

// LogPage is a page of one of the three logs, with the people the API suggests when the Following log is empty.
type LogPage struct {
	Page[Commit]
	IsRanked    bool
	Suggestions []User
}

func (p *LogPage) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &p.Page); err != nil {
		return err
	}

	var raw struct {
		IsRanked    bool   `json:"is_ranked"`
		Suggestions []User `json:"suggestions"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	p.IsRanked, p.Suggestions = raw.IsRanked, raw.Suggestions

	return nil
}

// Thread is a commit as its page shows it, with the commits it replies to, oldest first.
type Thread struct {
	Commit    Commit
	Ancestors []Commit
}

func (t *Thread) UnmarshalJSON(data []byte) error {
	var raw struct {
		Data      Commit   `json:"data"`
		Ancestors []Commit `json:"ancestors"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	t.Commit, t.Ancestors = raw.Data, raw.Ancestors

	return nil
}
