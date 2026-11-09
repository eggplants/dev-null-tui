package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// MaxScheduleAhead is the furthest ahead a commit can be scheduled.
const MaxScheduleAhead = 30 * 24 * time.Hour

// ScheduledCommit is a commit queued to go out at ScheduledAt. Only the user sees it until then. Body is nil once it
// went out, and Status is "scheduled" until it does, then the commit's own status, or "deleted".
type ScheduledCommit struct {
	ID              string     `json:"id"`
	Author          *User      `json:"author"`
	Body            *string    `json:"body"`
	BodySegments    []Segment  `json:"body_segments"`
	PreviewSegments []Segment  `json:"preview_segments"`
	PollOptions     []string   `json:"poll_options"`
	ImageCount      int        `json:"image_count"`
	ScheduledAt     time.Time  `json:"scheduled_at"`
	SubmittedAt     *time.Time `json:"submitted_at"`
	Status          string     `json:"status"`
}

// NewScheduledCommit is a commit to queue. It can't reply to or quote a commit.
type NewScheduledCommit struct {
	Body              string   `json:"body"`
	ScheduledAt       string   `json:"scheduled_at"`
	ImageDescriptions []string `json:"image_descriptions,omitempty"`
	Poll              *NewPoll `json:"poll,omitempty"`
	Author            string   `json:"author,omitempty"`
}

// Schedule queues a commit to go out at a time, with images if there are any.
func (c *Client) Schedule(ctx context.Context, draft NewCommit, at time.Time, images []File) (*ScheduledCommit, error) {
	in := NewScheduledCommit{
		Body:              draft.Body,
		ScheduledAt:       at.Format(time.RFC3339),
		ImageDescriptions: draft.ImageDescriptions,
		Poll:              draft.Poll,
		Author:            draft.Author,
	}

	var res data[ScheduledCommit]
	var err error
	if len(images) == 0 {
		err = c.Do(ctx, http.MethodPost, "/api/v1/scheduled-commits", in, &res)
	} else {
		err = c.DoFiles(ctx, http.MethodPost, "/api/v1/scheduled-commits", in, "images", images, &res)
	}

	return &res.Data, err
}

// ScheduledPage is a page of the user's queue. Pages are numbered from 1, and Next is 0 on the last.
type ScheduledPage struct {
	Data []ScheduledCommit `json:"data"`
	Meta struct {
		CurrentPage int `json:"current_page"`
		LastPage    int `json:"last_page"`
	} `json:"meta"`
}

// Scheduled lists the user's queue, 20 a page.
func (c *Client) Scheduled(ctx context.Context, page int) (*ScheduledPage, error) {
	var res ScheduledPage
	err := c.Do(ctx, http.MethodGet, "/api/v1/scheduled-commits?"+url.Values{"page": {strconv.Itoa(page)}}.Encode(), nil, &res)

	return &res, err
}

// CancelScheduled takes a commit off the queue before it goes out.
func (c *Client) CancelScheduled(ctx context.Context, id string) error {
	return c.Do(ctx, http.MethodDelete, "/api/v1/scheduled-commits/"+url.PathEscape(id), nil, nil)
}

// SubmitScheduled commits a queued commit now, before its time, through the usual review.
func (c *Client) SubmitScheduled(ctx context.Context, id string) (*ScheduledCommit, error) {
	var res data[ScheduledCommit]
	err := c.Do(ctx, http.MethodPost, "/api/v1/scheduled-commits/"+url.PathEscape(id)+"/submission", nil, &res)

	return &res.Data, err
}
