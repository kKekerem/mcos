package panel

import (
	"strings"
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
