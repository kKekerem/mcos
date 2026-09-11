#!/usr/bin/env python3
"""panel/ icinde TUI hizalamasini bozan glif kullanimi olmadigini dogrular.

Iki ayri hata sinifi denetlenir:

1. CIFT GENISLIKLI GLIFLER (east-asian-width W/F)
   go-runewidth ve lipgloss bunlari 2 kolon sayar, fbterm ise monospace olmayan
   bir yedek fontla cizer; gercek genislik oyunlanamaz.

2. VARIATION SELECTOR-16 (U+FE0F)  <-- daha sinsi olan
   Ornek: "⚠️" = U+26A0 + U+FE0F. U+26A0 Unicode'a gore TEK kolondur (eaw=N),
   bu yuzden 1. kontrol onu GECIRIR. Ama VS16 emoji sunumunu ZORLAR: terminal
   glifi IKI kolon cizerken lipgloss.Width bir sayar. Sonuc: her kullanimda
   1 kolon kayma ve bozuk kutu kenarlari.

   Bu hata gercekten olustu: OOBE disk kurulum ekranindaki "⚠️  DIKKAT" satiri
   sag kenarligi 1 kolon kaydirmisti.

Cikis kodu 1 ise en az bir ihlal var.
"""
import glob
import sys
import unicodedata
from unicodedata import east_asian_width as eaw

VS16 = "️"  # emoji sunumunu zorlar
VS15 = "︎"  # metin sunumunu zorlar (zararsiz ama tutarsiz)

wide_hits = 0
vs_hits = 0

for path in sorted(glob.glob("panel/**/*.go", recursive=True)):
    for lineno, line in enumerate(open(path, encoding="utf-8"), 1):

        # 1) Cift genislikli glifler
        wide = [c for c in line if ord(c) >= 0x2000 and eaw(c) in ("W", "F")]
        if wide:
            wide_hits += len(wide)
            glyphs = " ".join(f"{c} U+{ord(c):04X}" for c in dict.fromkeys(wide))
            print(f"{path}:{lineno}  [CIFT GENISLIK]")
            print(f"    glif : {glyphs}")
            print(f"    satir: {line.strip()[:100]}")

        # 2) Variation selector
        for vs, name in ((VS16, "VS16 U+FE0F"), (VS15, "VS15 U+FE0E")):
            if vs in line:
                vs_hits += line.count(vs)
                # Onundeki temel glifi de gosterelim.
                idx = line.index(vs)
                base = line[idx - 1] if idx > 0 else "?"
                try:
                    bname = unicodedata.name(base)
                except ValueError:
                    bname = "?"
                print(f"{path}:{lineno}  [{name}]")
                print(f"    temel glif : {base} U+{ord(base):04X} ({bname})")
                print(f"    satir      : {line.strip()[:100]}")

total = wide_hits + vs_hits
if total:
    print()
    if wide_hits:
        print(f"{wide_hits} cift genislikli glif kaldi.")
    if vs_hits:
        print(f"{vs_hits} variation selector kaldi.")
    print()
    print("Duzeltme: panel/theme/tokens.go icindeki tek kolonluk Icon* token'larini")
    print("kullanin. Uyari isareti icin theme.IconWarn (U+26A0, VS16 YOK).")
    sys.exit(1)

print("GECTI: panel/ icinde cift genislikli glif ve variation selector yok.")
