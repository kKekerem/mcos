#!/usr/bin/env python3
"""Aday TUI gliflerinin kolon genişliğini ölçer.

fbterm .fbtermrc icinde ambiguous-wide=0 ayarlidir; go-runewidth da varsayilan
olarak ambiguous glifleri 1 kolon sayar. Yani W/F disindaki her sinif 1 kolon
demektir. W veya F olan glifler TUI hizalamasini bozar ve token setinde
kullanilmamalidir.
"""
import sys
from unicodedata import east_asian_width as eaw, name

CANDIDATES = "◆◇●○■□▪▫▲▼►◄✓✗⚙ℹ⌁»«·◈◉◍▤▣▸▾★☰⟳⌂⚑─│━☑☐⬤"

EMOJI = "🎮⚡💾🔌🔍🔒👥🧩🌍🔐🌐📊📡⏳📋📶🗂"


def report(title, chars):
    print(title)
    print(f'  {"glif":4s} {"kod":9s} {"eaw":4s} {"kolon":5s} ad')
    bad = []
    for ch in chars:
        w = eaw(ch)
        cells = 2 if w in ("W", "F") else 1
        try:
            n = name(ch)
        except ValueError:
            n = "?"
        flag = "" if cells == 1 else "   <-- CIFT GENISLIK"
        print(f"  {ch:4s} U+{ord(ch):04X}   {w:4s} {cells:5d} {n}{flag}")
        if cells == 2:
            bad.append(ch)
    print()
    return bad


bad_cands = report("== ADAY TOKEN GLIFLERI ==", CANDIDATES)
report("== MEVCUT GORUNUMLERDEKI EMOJI (degistirilecek) ==", EMOJI)

if bad_cands:
    print("UYARI: su adaylar cift genislikli, token setine KOYULMAMALI:",
          " ".join(bad_cands))
    sys.exit(1)
print("Tum adaylar tek kolon — token setinde guvenle kullanilabilir.")
