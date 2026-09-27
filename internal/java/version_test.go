package java

import (
	"testing"

	"mcos/internal/model"
)

func TestRequiredJavaMajor(t *testing.T) {
	cases := []struct {
		version string
		want    int
	}{
		{"1.8.8", 8},
		{"1.12.2", 8},
		{"1.16.5", 8},
		{"1.17", 17},
		{"1.17.1", 17},
		{"1.18.2", 17},
		{"1.19.4", 17},
		{"1.20.4", 17},
		{"1.20.5", 21},
		{"1.20.6", 21},
		{"1.21", 21},
		{"1.21.4", 21},
		{"1.21-rc1", 21},
		{"1.21.11", 21},
		// Takvim sürümleri Java 25 ister (Mojang manifesti, 2026-09-26).
		{"26.1", 25},
		{"26.1.2", 25},
		{"26.3", 25},
		{"26.4-snapshot-1", 25},
		{"", 21},
		{"garbage", 21},
	}
	for _, c := range cases {
		if got := RequiredJavaMajor(c.version); got != c.want {
			t.Errorf("RequiredJavaMajor(%q) = %d, want %d", c.version, got, c.want)
		}
	}
}

// Eski eşlemeyle 21 kaydedilmiş bir 26.3 sunucusu 25'e çekilmeli; daha yeni
// bir seçim ve elle verilmiş yol ise korunmalı.
func TestRaiseToRequired(t *testing.T) {
	eski := &model.Server{MCVersion: "26.3", JavaMajor: 21}
	if !RaiseToRequired(eski) || eski.JavaMajor != 25 {
		t.Fatalf("26.3 sunucusu yükseltilmedi: %d", eski.JavaMajor)
	}
	yeni := &model.Server{MCVersion: "1.21.11", JavaMajor: 25}
	if RaiseToRequired(yeni) || yeni.JavaMajor != 25 {
		t.Fatalf("daha yeni Java düşürüldü: %d", yeni.JavaMajor)
	}
	elle := &model.Server{MCVersion: "26.3", JavaMajor: 21, JavaPath: "/opt/java/bin/java"}
	if RaiseToRequired(elle) || elle.JavaMajor != 21 {
		t.Fatalf("elle verilen Java yoluna dokunuldu: %d", elle.JavaMajor)
	}
}
