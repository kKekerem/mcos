package main

import (
	"testing"

	"mcos/internal/model"
)

// SSH'ta sunucu ADIYLA: tam ad (harf duyarsız) önekten önce gelir, tek
// anlamlı önek kabul edilir, belirsiz önek iki sonuç döner.
func TestEslesen(t *testing.T) {
	list := []*model.Server{{ID: "a1", Name: "Survival"}, {ID: "b2", Name: "Survival Test"}, {ID: "c3", Name: "Yaratıcı"}}
	if m := eslesen(list, "survival"); len(m) != 1 || m[0].ID != "a1" {
		t.Fatalf("tam ad: %+v", m)
	}
	if m := eslesen(list, "yara"); len(m) != 1 || m[0].ID != "c3" {
		t.Fatalf("önek: %+v", m)
	}
	if m := eslesen(list, "Surv"); len(m) != 2 {
		t.Fatalf("belirsiz önek iki sonuç vermeli: %+v", m)
	}
	if m := eslesen(list, "b2"); len(m) != 1 || m[0].Name != "Survival Test" {
		t.Fatalf("kimlik: %+v", m)
	}
}
