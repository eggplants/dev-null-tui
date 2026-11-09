package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// MinCommitSearch is the fewest characters a search for commits takes.
const MinCommitSearch = 3

// SearchCommits finds the commits nearest in meaning to q, closest first, as the web's search does. There may be fewer
// than limit once the irrelevant ones are left out.
func (c *Client) SearchCommits(ctx context.Context, q string, limit int) ([]Commit, error) {
	query := url.Values{"q": {q}, "limit": {strconv.Itoa(limit)}}

	var res data[[]Commit]
	err := c.Do(ctx, http.MethodGet, "/api/v1/search/commits?"+query.Encode(), nil, &res)

	return res.Data, err
}

// SearchProfiles finds the people and the organizations whose name or handle match q.
func (c *Client) SearchProfiles(ctx context.Context, q string) (people, organizations []User, err error) {
	var res struct {
		Users         []User `json:"users"`
		Organizations []User `json:"organizations"`
	}
	err = c.Do(ctx, http.MethodGet, "/api/v1/search/profiles?"+url.Values{"q": {q}}.Encode(), nil, &res)
	for i := range res.Organizations {
		res.Organizations[i].Type = ProfileOrganization
	}

	return res.Users, res.Organizations, err
}
