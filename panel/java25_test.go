package panel

import (
	"strings"
	"testing"
)

// Minecraft 26.x Java 25 ister. TUI panelinin Yazılım bölümünde Java 25
// satırı olmalı, imleç ona inebilmeli ve Java 21 satırı "1.21+ için" gibi
// 26.x'i de kapsıyormuş gibi görünmemeli (eski yazı: "Minecraft 1.20.5+ ve
// 1.21+ sunucuları için").
func TestTUIJava25Satiri(t *testing.T) {
	a := testApp()
	a.section = secSoftware
	a.focus = focusContent
	out := a.renderSoftware(110, 40)
	if !strings.Contains(out, "Java 25 (Temurin JDK)") || !strings.Contains(out, "26.1 ve sonrası") {
		t.Fatalf("Java 25 satırı çizilmedi:\n%s", out)
	}
	if strings.Contains(out, "1.21+") {
		t.Fatalf("Java 21 satırı hâlâ 26.x'i kapsıyor gibi: \n%s", out)
	}
	for i := 0; i < 10; i++ {
		a.moveCursor(1)
	}
	if got := javaRowMajors[a.rowCursor]; got != 25 {
		t.Fatalf("imleç en alta indiğinde Java %d; Java 25 bekleniyordu (imleç %d)", got, a.rowCursor)
	}
	if len(javaRowMajors) != len(javaTargets) {
		t.Fatal("satır listesi ile kurulum listesi ayrıştı")
	}
}
