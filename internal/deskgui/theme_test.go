package deskgui

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"mcos/internal/fbui"
)

// Pencere, MCOS panelinin paletini taşımalı. theme.css elle yazıldı; panel
// renkleri değişirse burası sessizce eskiyip iki ekran farklı ürün gibi
// görünürdü.
func TestTemaPanelPaletiyleAyni(t *testing.T) {
	p := fbui.DefaultPalette
	hex := func(c color.RGBA) string { return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B) }
	for ad, c := range map[string]color.RGBA{
		"--bg": p.Bg, "--surface": p.Surface, "--raised": p.Raised,
		"--border": p.Border, "--divider": p.Divider,
		"--text": p.Text, "--dim": p.TextDim, "--faint": p.TextFaint, "--on": p.TextOn,
		"--accent": p.Accent, "--ok": p.OK, "--warn": p.Warn, "--error": p.Error,
	} {
		want := ad + ": " + hex(c) + ";"
		if !strings.Contains(themeCSS, want) {
			t.Errorf("theme.css'te %q yok (panel paleti değişmiş olabilir)", want)
		}
	}
}
