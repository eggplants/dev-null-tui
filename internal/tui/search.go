package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// searchLimit is how many commits a search asks for, the most the API gives.
const searchLimit = 100

// newSearch shows what matches q: the commits nearest to it in meaning, as the web's search finds them, and the
// people and organizations whose name or handle match it.
func newSearch(app *App, q string) *tabbedView {
	t := newTabbedView(app, "Search")
	t.head = func(width int) []string {
		return []string{
			fmt.Sprintf(" [%s::b]$ grep[-::-] [%s]%s[-]", colorPrimary, colorInk, tview.Escape(firstLine(q, max(width-10, 10)))),
			"",
		}
	}

	commits := newCommitFeed(app, "Search", emptyState("no commits match "+q, "Search finds commits by meaning: try other words, or T for people."),
		func(ctx context.Context, cursor string) ([]*api.Commit, string, error) {
			if len([]rune(strings.TrimSpace(q))) < api.MinCommitSearch {
				return nil, "", nil
			}
			found, err := app.client.SearchCommits(ctx, q, searchLimit)
			if err != nil {
				return nil, "", err
			}

			return pointers(found), "", nil
		})
	if len([]rune(strings.TrimSpace(q))) < api.MinCommitSearch {
		commits.empty = emptyState(fmt.Sprintf("searching commits takes at least %d characters", api.MinCommitSearch), "T shows the people who match.")
	}
	commits.end = ""

	people := newPeopleList(app, "Search", emptyState("nobody matches "+q), func(ctx context.Context, cursor string) ([]*api.User, string, error) {
		users, orgs, err := app.client.SearchProfiles(ctx, q)
		if err != nil {
			return nil, "", err
		}

		return pointers(append(users, orgs...)), "", nil
	})

	t.addCommits("Commits", commits)
	t.addPeople("People", people)
	// Too short to search commits by, it can still find people.
	if len([]rune(strings.TrimSpace(q))) < api.MinCommitSearch {
		t.current = 1
	}

	return t
}
