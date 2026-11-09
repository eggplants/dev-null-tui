package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// SuggestedUsers are who to follow, as the web suggests them: three to show and three spares to fill the place of one
// dismissed. A later round suggests others.
func (c *Client) SuggestedUsers(ctx context.Context, round int) ([]User, error) {
	var res data[[]User]
	err := c.Do(ctx, http.MethodGet, "/api/v1/me/suggested-users?"+url.Values{"round": {strconv.Itoa(round)}}.Encode(), nil, &res)

	return res.Data, err
}

// DismissSuggestion says the user isn't interested in someone, who is never suggested again.
func (c *Client) DismissSuggestion(ctx context.Context, username string) error {
	return c.Do(ctx, http.MethodPost, "/api/v1/users/"+url.PathEscape(username)+"/dismiss", nil, nil)
}

// LiveFollowing are a few of the people the user follows who are live right now.
func (c *Client) LiveFollowing(ctx context.Context) ([]User, error) {
	var res data[[]User]
	err := c.Do(ctx, http.MethodGet, "/api/v1/me/live-following", nil, &res)

	return res.Data, err
}
