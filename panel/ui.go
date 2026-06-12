package panel

import (
	"fmt"
	"strings"

	"mcos/panel/theme"
)

// clampLines hard-limits a rendered block to at most h lines so a view can never
// overflow its pane and corrupt the surrounding layout (the "scroll breaks the
// TUI" bug). It is the last-resort safety net; lists should scroll themselves.
func clampLines(s string, h int) string {
	if h < 1 {
		h = 1
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= h {
		return s
	}
	return strings.Join(lines[:h], "\n")
}

// sliceScroll returns the window of body starting at line off, at most h lines.
func sliceScroll(body string, off, h int) string {
	if h < 1 {
		h = 1
	}
	lines := strings.Split(body, "\n")
	if off < 0 {
		off = 0
	}
	if off > len(lines)-1 {
		off = len(lines) - 1
	}
	if off < 0 {
		off = 0
	}
	lines = lines[off:]
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

// scrollList windows already-styled rows around cursor into exactly h lines,
// appending a "n-m / total" status line when the list does not fit. Keeps the
// selected row visible and never renders past h lines.
func scrollList(th *theme.Theme, rows []string, cursor, h int) string {
	n := len(rows)
	if h < 1 {
		h = 1
	}
	if n <= h {
		return strings.Join(rows, "\n")
	}
	visible := h - 1
	if visible < 1 {
		visible = 1
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor > n-1 {
		cursor = n - 1
	}
	top := cursor - visible/2
	if top < 0 {
		top = 0
	}
	if top > n-visible {
		top = n - visible
	}
	win := append([]string{}, rows[top:top+visible]...)
	status := th.Muted.Render(fmt.Sprintf("  %d-%d / %d  (yukarı/aşağı)", top+1, top+visible, n))
	return strings.Join(append(win, status), "\n")
}
