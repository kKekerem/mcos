#!/bin/sh
# test-install-tools.sh — kurulum/kalicilik betiklerinin cagirdigi HER harici
# komutun gercekten imajda oldugunu dogrular.
#
# ── Neden var ───────────────────────────────────────────────────────────────
# Bu betiklerin cagirdigi uc arac imajda YOKTU ve hepsi sahada patladi:
#   stat     (mcos-install, 3 yer)  -> "set -eu" ile kurulum yarida kaldi
#   sfdisk   (mcos-persist)         -> kalicilik hic olmadi
#   blockdev (ikisi de)             -> disk boyu 0 okundu
# Ayrica busybox'in blkid'i "-o value -s TYPE" secenegini TANIMIYOR; o secenege
# dayanan iki guvenlik denetimi sessizce hep geciyordu.
#
# Yontem: betiklerde KOMUT KONUMUNDAKI (satir basi, |, ;, &&, ||, $(, then, do,
# else, if, while, !) her kelime alinir. Kelime ana makinede bir program adiysa
# (yani yazar onu "var" sanmis olabilir) ama hedef kok dosya sisteminde YOKSA,
# betik icinde tanimli bir islev ya da kabuk yerlesigi degilse -> HATA.
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TARGET="$ROOT/os/buildroot/output/target"
OVERLAY="$ROOT/os/buildroot/external/board/mcos/rootfs-overlay/usr/bin"

if [ ! -d "$TARGET/bin" ]; then
    echo "ATLA: hedef kok dosya sistemi yok ($TARGET) — once 'make os'"
    exit 0
fi

exec python3 - "$TARGET" "$OVERLAY" "$@" <<'PY'
import os, re, shutil, sys

target, overlay = sys.argv[1], sys.argv[2]
betikler = sys.argv[3:] or ["mcos-install", "mcos-persist", "mcos-growdata", "mcos-vtoydata", "mcos-fwprune", "mcos-display", "mcos-launch", "mcos-findfs"]

YERLESIK = set("""
. : [ [[ alias bg break builtin cd command continue echo eval exec exit export
false fg getopts hash jobs kill let local printf pwd read readonly return set
shift test times trap true type ulimit umask unalias unset wait case esac if
then else elif fi for while until do done in function select time
""".split())

def hedefte(ad):
    for d in ("bin", "sbin", "usr/bin", "usr/sbin"):
        p = os.path.join(target, d, ad)
        if os.path.lexists(p):
            return True
    # overlay'deki betikler de imaja girer
    return os.path.exists(os.path.join(overlay, ad))

AYRAC = re.compile(r'(?:^|[|;&]|\$\(|`|\bthen\b|\bdo\b|\belse\b|\bif\b|\bwhile\b|\buntil\b|!)\s*([A-Za-z_][A-Za-z0-9_.+-]*)')

def komutlar(yol):
    metin = open(yol, encoding="utf-8", errors="replace").read()
    islevler = set(re.findall(r'^\s*([A-Za-z_][A-Za-z0-9_]*)\s*\(\)\s*\{', metin, re.M))
    # "command -v X" ile korunan araclar ISTEGE BAGLI: yoksa betik baska yola
    # dusuyor (ornek: mcos-findfs'te util-linux findfs).
    korunan = set(re.findall(r'command -v\s+([A-Za-z_][A-Za-z0-9_.+-]*)', metin))
    islevler |= korunan
    bulunan = {}
    heredoc = None
    for no, satir in enumerate(metin.splitlines(), 1):
        if heredoc:
            if satir.strip() == heredoc:
                heredoc = None
            continue
        m = re.search(r"<<-?\s*'?\"?([A-Z_]+)'?\"?", satir)
        if m:
            heredoc = m.group(1)
        s = satir.split("#", 1)[0] if not satir.lstrip().startswith("#") else ""
        # tirnak icindeki metni at: mesajlar komut degil
        s = re.sub(r'"[^"$`]*"', '""', s)
        s = re.sub(r"'[^']*'", "''", s)
        for k in AYRAC.findall(s):
            if re.match(r'^[A-Za-z_][A-Za-z0-9_]*=', k):
                continue
            if k in YERLESIK or k in islevler:
                continue
            bulunan.setdefault(k, no)
    return bulunan

hata = 0
for b in betikler:
    yol = os.path.join(overlay, b)
    if not os.path.exists(yol):
        print(f"  ATLA {b}: yok"); continue
    eksik = []
    for k, no in sorted(komutlar(yol).items()):
        if hedefte(k):
            continue
        if shutil.which(k) is None:
            continue  # ana makinede de yok: komut degil (degisken, metin parcasi)
        eksik.append(f"{k} (satir {no})")
    if eksik:
        hata += 1
        print(f"  HATA {b}: imajda OLMAYAN komut: {', '.join(eksik)}")
    else:
        print(f"  OK   {b}: cagrilan her harici komut imajda var")

# Busybox blkid'in tanimadigi secenekler
for b in betikler:
    yol = os.path.join(overlay, b)
    if not os.path.exists(yol):
        continue
    for no, satir in enumerate(open(yol, encoding="utf-8", errors="replace"), 1):
        s = satir.split("#", 1)[0]
        if re.search(r'\bblkid\b[^|;]*\s-(o|s|p)\b', s):
            hata += 1
            print(f"  HATA {b}:{no}: busybox blkid -o/-s/-p TANIMAZ (sessizce bos doner)")

# Gomulu onyukleyici ya da grub-install yedegi
bootlib = os.path.join(target, "usr/lib/mcos/boot")
gomulu = os.path.exists(os.path.join(bootlib, "core.img")) or os.path.exists(os.path.join(bootlib, "BOOTX64.EFI"))
yedek = hedefte("grub-install") and os.path.exists(os.path.join(target, "lib/grub/i386-pc/moddep.lst"))
if gomulu or yedek:
    print(f"  OK   onyukleyici: gomulu={'var' if gomulu else 'yok'} grub-install yedegi={'var' if yedek else 'yok'}")
else:
    hata += 1
    print("  HATA onyukleyici: ne gomulu parcalar ne grub-install + modul seti var — kurulum exit 1 verir")

if hata:
    print(f"{hata} test basarisiz"); sys.exit(1)
print("tum kurulum araci testleri gecti")
PY
