#!/usr/bin/env python3
"""Gomulu font adaylarinin UI glif kapsamasini olcer.

Neden: internal/fbfont testleri FiraCode Nerd Font'un token setindeki 11 glifi
ICERMEDIGINI ortaya cikardi. Bu, mevcut TUI'de de yasanan hizalama bozuklugunun
kaynagi: fbterm o glifler icin BASKA bir fonta dusuyor ve o fontun ilerlemesi
ayni olmayabiliyor.

Bu betik hangi fontun neyi kapsadigini kesin olarak raporlar, boylece "hangi
glifi kullanabilirim" sorusu tahminle degil olcumle cevaplanir.
"""
import glob
import os
import sys

try:
    from fontTools.ttLib import TTFont
except ImportError:
    print("fontTools yok. Kurulum: pip install fonttools", file=sys.stderr)
    sys.exit(2)

# Panelin kullandigi / kullanmayi dusundugumuz glifler, amaca gore gruplu.
GROUPS = {
    "metin-latin": "abcxyzABCXYZ0123456789",
    "metin-turkce": "ığüşöçİĞÜŞÖÇ",
    "noktalama": " .,:;!?()[]{}<>/\\|-_=+*#%&@'\"`~^$",
    "box-drawing": "─│┌┐└┘├┤┬┴┼╭╮╰╯━┃",
    "blok": "█▉▊▋▌▍▎▏░▒▓",
    "geometrik": "●○■□◆◇◈◉◍▪▫▤▣★▲▼◀▶▸◂▴▾",
    "simge": "⚑⚙⚠✓✗✦➜»«·…ℹ⌁⟳",
    "ok": "←↑→↓↔↕",
}

CANDIDATES = []
for pat in (
    "internal/fbfont/fonts/*.ttf",
    "os/buildroot/output/target/usr/share/fonts/DejaVuSansMono.ttf",
    "os/buildroot/output/target/usr/share/fonts/liberation/LiberationMono-Regular.ttf",
    "os/buildroot/output/target/usr/share/fonts/truetype/*.ttf",
):
    CANDIDATES.extend(sorted(glob.glob(pat)))

if not CANDIDATES:
    print("hic font bulunamadi", file=sys.stderr)
    sys.exit(2)

print(f"{'font':38s} {'grup':14s} {'kapsam':>10s}  eksikler")
print("-" * 100)

coverage = {}

for path in CANDIDATES:
    try:
        tt = TTFont(path, fontNumber=0, lazy=True)
        cmap = tt.getBestCmap()
    except Exception as e:  # noqa: BLE001
        print(f"{os.path.basename(path):38s} OKUNAMADI: {e}")
        continue

    name = os.path.basename(path)
    coverage[name] = set(cmap.keys())

    for group, chars in GROUPS.items():
        missing = [c for c in chars if ord(c) not in cmap]
        have = len(chars) - len(missing)
        mark = "TAM" if not missing else "".join(missing)
        print(f"{name:38s} {group:14s} {have:4d}/{len(chars):<5d}  {mark}")
    print()
    tt.close()

# Birlesik kapsama: bir yedek zinciri her seyi kapatir mi?
print("=" * 100)
print("YEDEK ZINCIRI ANALIZI")
print()
allchars = "".join(GROUPS.values())
names = list(coverage.keys())
for i, primary in enumerate(names):
    union = set(coverage[primary])
    chain = [primary]
    for other in names:
        if other == primary:
            continue
        missing_now = {ord(c) for c in allchars} - union
        adds = missing_now & coverage[other]
        if adds:
            union |= coverage[other]
            chain.append(other)
    still = [c for c in allchars if ord(c) not in union]
    print(f"birincil: {primary}")
    print(f"  zincir : {' -> '.join(chain)}")
    if still:
        print(f"  HALA EKSIK ({len(still)}): {''.join(still)}")
    else:
        print("  TUM GLIFLER KAPSANIYOR")
    print()
    if i >= 1:
        break
