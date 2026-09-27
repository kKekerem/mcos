package perfpack

import (
	"fmt"
	"strings"
)

// ════════════════════════════════════════════════════════════════════════════
// KÜÇÜK YAML YAMACI
// ════════════════════════════════════════════════════════════════════════════
//
// Paper/Spigot ayar dosyalarında YALNIZCA birkaç yaprak değer değiştirilir.
// Dosyayı bir YAML kütüphanesiyle okuyup baştan yazmak iki şeyi bozardı:
// Paper'ın açıklama satırlarını (kullanıcının dosyada okuduğu belgeler) ve
// kullanıcının elle yaptığı düzeni. Ayrıca go.mod'a yeni bağımlılık eklemek
// gerekirdi. Burada satır satır çalışılır: yalnızca hedef satır değişir, eksik
// anahtar ebeveyninin bloğunun sonuna eklenir; geri kalan her bayt aynı kalır.
//
// Desteklenen biçim Paper'ın (Configurate/SnakeYAML) ve Bukkit'in yazdığı blok
// biçimidir: girintili "anahtar: değer", "#" açıklamaları, "- " liste
// öğeleri, boş eşlem için "{}". Liste İÇİNDEN geçen yollar desteklenmez;
// paketin hiçbir ayarı buna ihtiyaç duymaz.

// yline is one "key: value" line of a YAML document.
type yline struct {
	idx    int    // satır numarası (0'dan)
	indent int    // baştaki boşluk sayısı
	key    string // tırnaksız anahtar
	raw    string // satırdaki anahtar (tırnaklarıyla)
	value  string // ":" sonrası, kırpılmış
	path   []string
}

// parseYAML indexes the key lines of doc and their full paths.
func parseYAML(lines []string) []yline {
	var out []yline
	type frame struct {
		indent int
		key    string
	}
	var stack []frame
	for i, l := range lines {
		l = strings.TrimRight(l, "\r")
		t := strings.TrimLeft(l, " ")
		if t == "" || strings.HasPrefix(t, "#") || t == "-" || strings.HasPrefix(t, "- ") {
			continue
		}
		indent := len(l) - len(t)
		raw, key, rest, ok := splitKey(t)
		if !ok {
			continue // çok satırlı bir değerin devamı
		}
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		path := make([]string, 0, len(stack)+1)
		for _, f := range stack {
			path = append(path, f.key)
		}
		path = append(path, key)
		out = append(out, yline{idx: i, indent: indent, key: key, raw: raw,
			value: strings.TrimSpace(rest), path: path})
		stack = append(stack, frame{indent: indent, key: key})
	}
	return out
}

// splitKey splits `key: value` (key may be quoted).
func splitKey(t string) (raw, key, rest string, ok bool) {
	if t[0] == '\'' || t[0] == '"' {
		q := t[0]
		end := strings.IndexByte(t[1:], q)
		if end < 0 {
			return "", "", "", false
		}
		raw = t[:end+2]
		after := t[end+2:]
		if !strings.HasPrefix(after, ":") {
			return "", "", "", false
		}
		return raw, t[1 : end+1], after[1:], true
	}
	for i := 0; i < len(t); i++ {
		if t[i] == ':' && (i+1 == len(t) || t[i+1] == ' ') {
			return t[:i], t[:i], t[i+1:], true
		}
	}
	return "", "", "", false
}

func samePath(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// unquote strips YAML scalar quotes ('default' -> default).
func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '\'' || v[0] == '"') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// yamlGet returns the scalar at path.
//
// present=false: anahtar yok. Anahtar bir eşlemse (altında çocuklar var)
// değer boş döner ve isMap=true.
func yamlGet(doc string, path []string) (value string, present, isMap bool) {
	lines := strings.Split(doc, "\n")
	for _, y := range parseYAML(lines) {
		if samePath(y.path, path) {
			if y.value == "" || y.value == "{}" {
				return "", true, true
			}
			return unquote(y.value), true, false
		}
	}
	return "", false, false
}

// yamlSet sets the scalar at path to value, creating missing parents.
func yamlSet(doc string, path []string, value string) (string, error) {
	if len(path) == 0 {
		return doc, fmt.Errorf("boş yol")
	}
	// Sondaki yeni satır ayrı tutulur: ekleme son satırın ARKASINA yapılmalı,
	// boş "son satırın" arkasına değil.
	trail := strings.HasSuffix(doc, "\n")
	body := strings.TrimSuffix(doc, "\n")
	var lines []string
	if body != "" {
		lines = strings.Split(body, "\n")
	}
	keys := parseYAML(lines)

	// 1) Anahtar zaten varsa yalnızca değerini değiştir.
	for _, y := range keys {
		if !samePath(y.path, path) {
			continue
		}
		if y.value == "" && hasChildren(keys, y) {
			return doc, fmt.Errorf("%s bir eşlem, değer yazılamaz", strings.Join(path, "."))
		}
		cr := ""
		if strings.HasSuffix(lines[y.idx], "\r") {
			cr = "\r"
		}
		lines[y.idx] = strings.Repeat(" ", y.indent) + y.raw + ": " + value + cr
		return join(lines, trail), nil
	}

	// 2) En derin var olan ebeveyni bul.
	var parent *yline
	depth := 0
	for d := len(path) - 1; d >= 1 && parent == nil; d-- {
		for i := range keys {
			if samePath(keys[i].path, path[:d]) {
				parent, depth = &keys[i], d
				break
			}
		}
	}

	// 3) Eklenecek satırlar ve yeri.
	insertAt := len(lines)
	childIndent := 0
	if parent != nil {
		switch {
		case parent.value == "{}":
			// Boş eşlem: "{}" kaldırılır, çocuklar altına eklenir.
			lines[parent.idx] = strings.Repeat(" ", parent.indent) + parent.raw + ":"
		case parent.value != "":
			return doc, fmt.Errorf("%s bir değer, altına anahtar eklenemez",
				strings.Join(path[:depth], "."))
		}
		childIndent = parent.indent + 2
		insertAt = parent.idx + 1
		first := true
		for _, y := range keys {
			if y.idx <= parent.idx {
				continue
			}
			if y.indent <= parent.indent {
				break
			}
			if first {
				childIndent, first = y.indent, false
			}
		}
		// Bloğun son satırı: ebeveynden daha girintili son satır (açıklama
		// ve liste öğeleri dahil), bir sonraki kardeş/üst anahtardan önce.
		for i := parent.idx + 1; i < len(lines); i++ {
			t := strings.TrimLeft(strings.TrimRight(lines[i], "\r"), " ")
			if t == "" {
				continue
			}
			ind := len(strings.TrimRight(lines[i], "\r")) - len(t)
			if ind <= parent.indent {
				break
			}
			insertAt = i + 1
		}
	}
	var add []string
	for d := depth; d < len(path); d++ {
		ind := childIndent + (d-depth)*2
		if d == len(path)-1 {
			add = append(add, strings.Repeat(" ", ind)+path[d]+": "+value)
		} else {
			add = append(add, strings.Repeat(" ", ind)+path[d]+":")
		}
	}
	out := make([]string, 0, len(lines)+len(add))
	out = append(out, lines[:insertAt]...)
	out = append(out, add...)
	out = append(out, lines[insertAt:]...)
	return join(out, trail || body == ""), nil
}

func hasChildren(keys []yline, p yline) bool {
	for _, y := range keys {
		if y.idx > p.idx {
			return y.indent > p.indent
		}
	}
	return false
}

func join(lines []string, trail bool) string {
	s := strings.Join(lines, "\n")
	if trail {
		s += "\n"
	}
	return s
}
