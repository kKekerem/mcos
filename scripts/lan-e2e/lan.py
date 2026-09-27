#!/usr/bin/env python3
"""İki sanal makineli MCOS yerel ağ sınama düzeneği.

Ön koşul: derlenmiş imaj (os/buildroot/output/images: bzImage, rootfs.cpio.gz),
qemu-system-x86_64 ve KVM. İmaj YENİDEN DERLENMEZ: taze Go ikilileri ve
dist/mods/link, üstüne eklenen küçük bir cpio (overlay) ile gelir.
Unix soket yolları 108 baytı aşmasın diye MCOS_LAN_WORK kısa tutulmalı.

Her VM: bzImage + (rootfs.cpio.gz ⊕ overlay.cpio.gz). Overlay taze Go
ikililerini ve ttyS0'da bir kabuğu getirir. İki ağ kartı:
  eth-lan : -netdev socket,mcast  -> iki VM aynı "yerel ağda" (DHCP yok, sabit IP)
  eth-wan : -netdev user          -> internet (sunucu jar'ı indirmek için) + hostfwd

Kullanım:
  lan.py overlay               ikilileri derle, overlay.cpio.gz üret
  lan.py up A|B [--ram MB]     VM'i başlat (arka planda), kabuğu bekle
  lan.py sh A|B 'komut' [--t sn]   seri kabukta komut çalıştır, çıktı+kod
  lan.py down A|B              VM'i PID ile kapat
"""
import os, sys, socket, subprocess, time, random, shutil, argparse, json, tempfile

# Depo kökü betiğin yerinden çıkar; çalışma dosyaları (overlay, soketler,
# pid'ler, initrd'ler) MCOS_LAN_WORK altına yazılır — depoya değil.
ROOT = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "../.."))
HERE = os.environ.get("MCOS_LAN_WORK") or os.path.join(tempfile.gettempdir(), "mcos-lan-e2e")
os.makedirs(HERE, exist_ok=True)
IMG = os.path.join(ROOT, "os/buildroot/output/images")
STAGE = os.path.join(HERE, "stage")
OVL = os.path.join(HERE, "overlay.cpio.gz")

VMS = {
    # ad: (lan mac, wan mac, lan ip, host->misafir port yönlendirmeleri)
    "A": ("52:54:00:77:0a:01", "52:54:00:77:0b:01", "192.168.77.10",
          [(25611, 25565), (25612, 25566), (27911, 27892)]),
    "B": ("52:54:00:77:0a:02", "52:54:00:77:0b:02", "192.168.77.20",
          [(25621, 25565), (25622, 25566), (27921, 27892)]),
}
MCAST = "230.77.0.1:47700"


def sh(cmd, **kw):
    print("+", cmd, flush=True)
    return subprocess.run(cmd, shell=True, check=True, **kw)


def overlay(extra_bins=()):
    shutil.rmtree(STAGE, ignore_errors=True)
    b = os.path.join(STAGE, "usr/bin")
    os.makedirs(b)
    env = dict(os.environ, CGO_ENABLED="0", GOOS="linux", GOARCH="amd64")
    for name in ("mcosd", "mcosctl", "mcos-node", "mcos-panel-fb"):
        subprocess.run(["go", "build", "-o", os.path.join(b, name), "./cmd/" + name],
                       cwd=ROOT, env=env, check=True)
    # Ortak dünya jar'ları (varsa) imajdaki yere.
    link = os.path.join(ROOT, "dist/mods/link")
    if os.path.isdir(link):
        dst = os.path.join(STAGE, "usr/lib/mcos/mods/link")
        os.makedirs(dst)
        for f in os.listdir(link):
            # Gerçek imaj gibi: yalnızca mod jar'ları + dizin RAM'e girer;
            # fabric-api ISO'daki çevrimdışı pakette / internetten gelir.
            if (f.startswith("mcos-link") and f.endswith(".jar")) or f.endswith(".tsv"):
                shutil.copy2(os.path.join(link, f), dst)
    # inittab: özgün + ttyS0 kabuğu.
    tgt = os.path.join(ROOT, "os/buildroot/output/target/etc/inittab")
    os.makedirs(os.path.join(STAGE, "etc"))
    with open(tgt) as f:
        it = f.read()
    it += "\nttyS0::respawn:/bin/sh -l\n"
    with open(os.path.join(STAGE, "etc/inittab"), "w") as f:
        f.write(it)
    sh(f"cd {STAGE} && find . | cpio -o -H newc -R 0:0 --quiet | gzip -1 > {OVL}")
    print("overlay:", OVL, os.path.getsize(OVL))


def sockpath(vm):
    return os.path.join(HERE, f"s{vm}.sock")


def pidpath(vm):
    return os.path.join(HERE, f"q{vm}.pid")


def up(vm, ram):
    lanmac, wanmac, _, fwds = VMS[vm]
    initrd = os.path.join(HERE, f"initrd-{vm}.gz")
    with open(initrd, "wb") as out:
        for part in (os.path.join(IMG, "rootfs.cpio.gz"), OVL):
            with open(part, "rb") as f:
                shutil.copyfileobj(f, out)
    hf = ",".join(f"hostfwd=tcp::{h}-:{g}" for h, g in fwds)
    s = sockpath(vm)
    if os.path.exists(s):
        os.unlink(s)
    cmd = [
        "qemu-system-x86_64", "-enable-kvm", "-cpu", "host", "-m", str(ram), "-smp", "4",
        "-kernel", os.path.join(IMG, "bzImage"), "-initrd", initrd,
        "-append", "console=tty0 console=ttyS0,115200 consoleblank=0 loglevel=4 fbcon=nodefer mcos.live",
        "-vga", "std", "-display", "none",
        "-serial", f"unix:{s},server=on,wait=off",
        "-netdev", f"socket,id=lan,mcast={MCAST}",
        "-device", f"virtio-net-pci,netdev=lan,mac={lanmac}",
        "-netdev", f"user,id=wan,{hf}",
        "-device", f"virtio-net-pci,netdev=wan,mac={wanmac}",
        "-pidfile", pidpath(vm), "-daemonize",
    ]
    subprocess.run(cmd, check=True)
    # Kabuk gelene kadar bekle.
    t0 = time.time()
    while time.time() - t0 < 240:
        try:
            out, code = run(vm, "echo hazir", timeout=5)
            if code == 0 and "hazir" in out:
                print(f"{vm}: kabuk hazır ({time.time()-t0:.0f} sn)")
                return
        except Exception:
            pass
        time.sleep(3)
    raise SystemExit(f"{vm}: kabuk gelmedi")


def run(vm, command, timeout=60):
    tag = "MK%06d" % random.randrange(10**6)
    # İşaret, komutun yankısında EŞLEŞMESİN diye tırnakla bölünür.
    split = tag[:2] + "''" + tag[2:]
    s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    s.settimeout(1.0)
    s.connect(sockpath(vm))
    try:
        # Yankıyı kapat: uzun komutlar 80 sütunda kırılıp yankıda bölününce
        # işaret araması şaşıyordu (ilk denemede görüldü).
        s.sendall(b"stty -echo 2>/dev/null\n")
        time.sleep(0.3)
        try:
            while s.recv(65536):
                pass
        except socket.timeout:
            pass
        line = f"{command}; echo {split}:$?\n"
        s.sendall(line.encode())
        buf = b""
        t0 = time.time()
        while time.time() - t0 < timeout:
            try:
                chunk = s.recv(65536)
                if not chunk:
                    break
                buf += chunk
            except socket.timeout:
                pass
            txt = buf.decode("utf-8", "replace")
            i = txt.find(tag + ":")
            if i >= 0:
                j = txt.find("\n", i)
                if j < 0:
                    continue
                code = int(txt[i + len(tag) + 1:j].strip() or "0")
                return txt[:i].replace("\r", ""), code
        raise TimeoutError(f"{vm}: zaman aşımı: {command[:80]}")
    finally:
        s.close()


def down(vm):
    p = pidpath(vm)
    if not os.path.exists(p):
        return
    pid = int(open(p).read().strip())
    try:
        os.kill(pid, 15)
        for _ in range(50):
            os.kill(pid, 0)
            time.sleep(0.1)
        os.kill(pid, 9)
    except ProcessLookupError:
        pass
    if os.path.exists(p):
        os.unlink(p)
    print(f"{vm}: kapatıldı (pid {pid})")


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("op")
    ap.add_argument("vm", nargs="?")
    ap.add_argument("cmd", nargs="?")
    ap.add_argument("--ram", type=int, default=3072)
    ap.add_argument("--t", type=int, default=60)
    a = ap.parse_args()
    if a.op == "overlay":
        overlay()
    elif a.op == "up":
        up(a.vm, a.ram)
    elif a.op == "sh":
        out, code = run(a.vm, a.cmd, a.t)
        sys.stdout.write(out)
        print(f"[çıkış {code}]")
        sys.exit(0 if code == 0 else 1)
    elif a.op == "down":
        down(a.vm)
    else:
        raise SystemExit("bilinmeyen işlem")
