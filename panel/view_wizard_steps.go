package panel

import (
	"fmt"
	"strings"

	"mcos/internal/model"
	"mcos/panel/theme"
)

// wizTemplate is a quick-start preset that prefills the create wizard.
type wizTemplate struct {
	label      string
	custom     bool // "Özel" — change nothing
	software   model.Software
	gamemode   string
	difficulty string
	pvp        bool
	whitelist  bool
	hardcore   bool
	viaVersion bool
	note       string
}

var templates = []wizTemplate{
	{label: "Özel (kendim ayarlarım)", custom: true, note: "Hiçbir alan değiştirilmez."},
	{label: "Survival — Paper", software: model.SoftwarePaper, gamemode: "survival", difficulty: "normal", pvp: true, note: "Eklenti destekli, dengeli hayatta kalma."},
	{label: "Yaratıcı — Fabric", software: model.SoftwareFabric, gamemode: "creative", difficulty: "peaceful", note: "Mod destekli, serbest inşa."},
	{label: "Vanilla — Resmi", software: model.SoftwareVanilla, gamemode: "survival", difficulty: "easy", pvp: true, note: "Saf Minecraft deneyimi."},
	{label: "Anarşi — Paper", software: model.SoftwarePaper, gamemode: "survival", difficulty: "hard", pvp: true, note: "Kuralsız, zorlu PvP (whitelist kapalı)."},
	{label: "Hardcore — Vanilla", software: model.SoftwareVanilla, gamemode: "survival", difficulty: "hard", pvp: true, hardcore: true, note: "Tek can, kalıcı ölüm."},
}

// applyTemplate copies the selected preset's defaults into the wizard. "Özel"
// is a no-op so the user keeps whatever they already chose.
func (w *wizardModel) applyTemplate() {
	t := templates[w.templateIdx]
	if t.custom {
		return
	}
	for i, s := range model.AllSoftware {
		if s == t.software {
			w.softwareIdx = i
			break
		}
	}
	for i, g := range gamemodes {
		if g == t.gamemode {
			w.gamemodeIdx = i
			break
		}
	}
	for i, d := range difficulties {
		if d == t.difficulty {
			w.difficultyIdx = i
			break
		}
	}
	w.pvp = t.pvp
	w.whitelist = t.whitelist
	w.hardcore = t.hardcore
	w.viaVersion = t.viaVersion
	if len(w.versions) > 0 {
		w.verIdx = 0 // newest
	}
}

func (w *wizardModel) viewTemplate(th *theme.Theme) string {
	t := templates[w.templateIdx]
	return strings.Join([]string{
		w.row("Şablon", "‹ "+t.label+" ›", w.cursor == 0),
		"",
		th.Muted.Render(t.note),
		th.Muted.Render("Şablon makul varsayılanlar doldurur; sonraki adımlarda değiştirebilirsiniz."),
	}, "\n")
}

func (w *wizardModel) viewGameplay(th *theme.Theme) string {
	return strings.Join([]string{
		w.row("Online mode", w.toggle(w.onlineMode), w.cursor == 0),
		w.row("Oyun modu", "‹ "+gamemodes[w.gamemodeIdx]+" ›", w.cursor == 1),
		w.row("Zorluk", "‹ "+difficulties[w.difficultyIdx]+" ›", w.cursor == 2),
		w.row("PvP", w.toggle(w.pvp), w.cursor == 3),
		w.row("Maks oyuncu", w.maxPlayers.View(), w.cursor == 4),
		w.row("Beyaz liste", w.toggle(w.whitelist), w.cursor == 5),
		w.row("Hardcore", w.toggle(w.hardcore), w.cursor == 6),
		w.row("MOTD", w.motd.View(), w.cursor == 7),
		"",
		th.Muted.Render("Online mode kapalı = lisanssız (cracked) istemcilere izin verir."),
	}, "\n")
}

func (w *wizardModel) viewEULA(th *theme.Theme) string {
	box := th.Badge(" REDDEDİLDİ ", th.P.Red)
	if w.eulaAccepted {
		box = th.Badge(" KABUL EDİLDİ ", th.P.Green)
	}
	return strings.Join([]string{
		w.row("Mojang EULA", box, w.cursor == 0),
		"",
		th.Muted.Render("Minecraft sunucusu çalıştırmak için Mojang Son Kullanıcı"),
		th.Muted.Render("Lisans Sözleşmesi'ni kabul etmeniz gerekir:"),
		th.Accent.Render("https://aka.ms/MinecraftEULA"),
		"",
		th.Muted.Render(fmt.Sprintf("Kabul etmek için space/←→ — durum: %v", w.eulaAccepted)),
	}, "\n")
}
