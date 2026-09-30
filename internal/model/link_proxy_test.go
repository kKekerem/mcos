package model

import "testing"

// Sözleşme: ad küçük harf, [a-z0-9_-] dışı "-" (Java tarafı adı topolojiden
// okur; kurucunun velocity.toml'u da aynı adı kullanır).
func TestProxyBackendName(t *testing.T) {
	for in, want := range map[string]string{
		"PC-B":        "pc-b",
		"mcos kutu-2": "mcos-kutu-2",
		"mcos_1":      "mcos_1",
		"Ev.PC":       "ev-pc",
		"":            "node",
	} {
		if got := ProxyBackendName(in); got != want {
			t.Errorf("ProxyBackendName(%q) = %q, beklenen %q", in, got, want)
		}
	}
	got := ProxyBackendNames([]string{"PC B", "pc-b", "mcos"})
	if got[0] != "pc-b" || got[1] != "pc-b-2" || got[2] != "mcos" {
		t.Fatalf("aynı ada düşen düğümler ayrılmalı: %v", got)
	}
}

func TestBackendProxyFromSpec(t *testing.T) {
	spec := LinkSpec{ProxySecret: "s3cret", Rules: &LinkRules{OnlineMode: false}}
	px := spec.BackendProxy(SoftwarePaper)
	if px == nil || px.Secret != "s3cret" || px.OnlineMode || px.PublicPort != 0 {
		t.Fatalf("eş arka ucu yanlış: %+v", px)
	}
	if spec.BackendProxy(SoftwareSpigot) != nil {
		t.Fatal("Spigot modern yönlendirmeyi bilmez; proxy arkasına alınmamalı")
	}
	if (LinkSpec{}).BackendProxy(SoftwareFabric) != nil {
		t.Fatal("anahtar yoksa proxy yok")
	}
}
