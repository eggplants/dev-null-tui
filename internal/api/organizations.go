package api

import (
	"context"
	"net/http"
	"net/url"
)

// Organization is an organization's page, as a signed request sees it. Organizations share the "@handle" pages
// with users.
type Organization struct {
	Data struct {
		User
		// Admins are the people who run the organization's page.
		Admins []User `json:"admins"`
	} `json:"data"`
	IsFollowing bool `json:"is_following"`
	// CanClaim tells whether the user can claim the page, as an admin of the organization on GitHub.
	CanClaim bool `json:"can_claim"`
}

func organizationPath(handle string) string {
	return "/api/v1/organizations/" + url.PathEscape(handle)
}

func (c *Client) Organization(ctx context.Context, handle string) (*Organization, error) {
	var res Organization
	err := c.Do(ctx, http.MethodGet, organizationPath(handle), nil, &res)
	res.Data.Type = ProfileOrganization

	return &res, err
}

// OrganizationCommits lists what the organization committed, or the commits mentioning it when mentions is set.
func (c *Client) OrganizationCommits(ctx context.Context, handle, cursor string, mentions bool) (*Page[Commit], error) {
	path := organizationPath(handle) + "/commits"
	if mentions {
		path = organizationPath(handle) + "/mentions"
	}

	var res Page[Commit]
	err := c.Do(ctx, http.MethodGet, withCursor(path, cursor, nil), nil, &res)

	return &res, err
}

func (c *Client) FollowOrganization(ctx context.Context, handle string, follow bool) error {
	return c.Do(ctx, toggle(follow), organizationPath(handle)+"/follow", nil, nil)
}

// FollowedOrganizations lists the organizations a user follows, newest follow first.
func (c *Client) FollowedOrganizations(ctx context.Context, username, cursor string) (*Page[User], error) {
	page, err := c.people(ctx, "/api/v1/users/"+url.PathEscape(username)+"/following/organizations", cursor)
	if page != nil {
		for i := range page.Data {
			page.Data[i].Type = ProfileOrganization
		}
	}

	return page, err
}

// ClaimOrganization claims an organization's page for the user, which GitHub confirms they belong to. Someone else
// claiming it in between leaves it theirs.
func (c *Client) ClaimOrganization(ctx context.Context, handle string) error {
	return c.Do(ctx, http.MethodPost, organizationPath(handle)+"/claim", nil, nil)
}
