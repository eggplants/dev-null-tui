package tui

import (
	"context"
	"fmt"

	"github.com/rivo/tview"
)

// pager is a listView that loads its items a page at a time, loading the next page when the last item is selected.
type pager[T any] struct {
	*listView[T]

	app   *App
	title string
	// empty is what the list says when it has nothing in it, as lines to show: a fatal: line and what to do.
	empty []string
	// end is the line under the last item once there are no more.
	end  string
	load func(ctx context.Context, cursor string) (items []T, next string, err error)

	cursor  string
	loaded  bool
	loading bool
	// generation tells answers to an earlier Reload apart, so they don't land in the list after it was reloaded.
	generation int
	// afterReload runs on the UI goroutine once the first page is in.
	afterReload func()
	// keep says which item of a reload to select, so the selection stays on what the reader was on.
	keep func(items []T) int
	// top gives the items that go above the first page, as a profile's pinned commit does.
	top func() []T
}

func newPager[T any](app *App, title string, empty []string, render func(T, bool, int) []string) *pager[T] {
	p := &pager[T]{listView: newListView(render), app: app, title: title, empty: empty, end: endOfList()}
	p.onLast = p.loadMore

	return p
}

// emptyState is what an empty list says: a fatal: line, as git would print it, and the hints under it.
func emptyState(fatal string, hints ...string) []string {
	lines := []string{"", fmt.Sprintf("   [%s]fatal:[-] [%s]%s[-]", colorDanger, colorInk, tview.Escape(fatal))}
	for _, h := range hints {
		lines = append(lines, fmt.Sprintf("   [%s]%s[-]", colorMuted, h))
	}

	return lines
}

// endOfList closes a log the way the web does, with the hollow node of the root commit.
func endOfList() string {
	return fmt.Sprintf(" [%s]○[-] [%s]That's the root commit. There's nothing before it.[-]", colorRail, colorFaint)
}

func (p *pager[T]) Title() string { return p.title }
func (p *pager[T]) Loaded() bool  { return p.loaded }
func (p *pager[T]) Loading() bool { return p.loading }

func (p *pager[T]) Reload() {
	p.generation++
	p.cursor, p.loaded = "", false
	p.fetch(true)
}

func (p *pager[T]) loadMore() {
	if p.loaded && p.cursor != "" {
		p.fetch(false)
	}
}

func (p *pager[T]) fetch(reset bool) {
	if p.loading && !reset {
		return
	}
	p.loading = true
	p.SetFooter(fmt.Sprintf("   [%s]Reading from /dev/null…[-]", colorMuted))
	p.app.spin()

	generation, cursor := p.generation, p.cursor
	var items []T
	var next string
	p.app.async(func(ctx context.Context) (err error) {
		items, next, err = p.load(ctx, cursor)
		return err
	}, func(err error) {
		if generation != p.generation {
			return
		}
		p.loading = false

		if err != nil {
			p.SetFooter(fmt.Sprintf("   [%s]%s[-]", colorDanger, tview.Escape(errorMessage(err))), fmt.Sprintf("   [%s]Press R to try again.[-]", colorMuted))
			p.app.flashError(err)
			return
		}

		if reset {
			if p.top != nil {
				items = append(p.top(), items...)
			}
			selected := 0
			if p.keep != nil && len(p.items) > 0 {
				selected = p.keep(items)
			}
			p.selected, p.offset = 0, 0
			p.SetItems(items)
			p.selected = max(0, min(selected, len(items)-1))
		} else {
			p.Append(items)
		}
		p.cursor, p.loaded = next, true

		switch {
		case len(p.items) == 0:
			p.SetFooter(p.empty...)
		case next == "":
			p.SetFooter(p.end)
		default:
			p.SetFooter()
		}

		if reset && p.afterReload != nil {
			p.afterReload()
		}
		// The selection may already sit on the last item, as on a short first page.
		if p.selected == len(p.items)-1 && next != "" {
			p.loadMore()
		}
	})
}

// Remove takes an item out of the list, as when its commit was deleted.
func (p *pager[T]) Remove(index int) {
	if index < 0 || index >= len(p.items) {
		return
	}

	p.SetItems(append(p.items[:index], p.items[index+1:]...))
	if len(p.items) == 0 && p.loaded {
		p.SetFooter(p.empty...)
	}
}
