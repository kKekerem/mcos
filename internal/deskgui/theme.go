package deskgui

import _ "embed"

// Ortak görünüm: MCOS panelinin grafit/turkuaz paleti (internal/fbui/theme.go
// DefaultPalette). Renkler oradan BİREBİR alındı: kullanıcı MCOS ekranında
// ve Windows penceresinde aynı ürünü görmeli.
//
// theme_test.go, buradaki değişkenlerin fbui paletiyle aynı kaldığını denetler;
// panel renkleri değişirse bu dosya sessizce eskimesin.

//go:embed theme.css
var themeCSS string

//go:embed base.js
var baseJS string
