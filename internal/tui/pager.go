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
	empty string
	load  func(ctx context.Context, cursor string) (items []T, next string, err error)

	cursor  string
	loaded  bool
	loading bool
	// generation tells answers to an earlier Reload apart, so they don't land in the list after it was reloaded.
	generation int
	// afterReload runs on the UI goroutine once the first page is in.
	afterReload func()
}

func newPager[T any](app *App, title, empty string, render func(T, bool, int) []string) *pager[T] {
	p := &pager[T]{listView: newListView(render), app: app, title: title, empty: empty}
	p.onLast = p.loadMore

	return p
}

func (p *pager[T]) Loaded() bool { return p.loaded }

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
	p.SetFooter(fmt.Sprintf("[%s]Loading…[-]", colorMuted))

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
			p.SetFooter(fmt.Sprintf("[red]%s[-]", tview.Escape(err.Error())), fmt.Sprintf("[%s]Press R to try again.[-]", colorMuted))
			p.app.flashError(err)
			return
		}

		if reset {
			p.selected, p.offset = 0, 0
			p.SetItems(items)
		} else {
			p.Append(items)
		}
		p.cursor, p.loaded = next, true

		switch {
		case len(p.items) == 0:
			p.SetFooter(fmt.Sprintf("[%s]%s[-]", colorMuted, p.empty))
		case next == "":
			p.SetFooter(fmt.Sprintf("[%s]── end ──[-]", colorMuted))
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
}
