#!/usr/bin/env python3
"""MCOS yerel ağ uçtan uca sınaması: A = MCOS kutusu, B = PC (mcos-node).

Adımlar (her biri ölçülür ve raporlanır):
  1. İki VM açılır, LAN kartlarına sabit IP verilir (DHCP'siz ev ağı gibi).
  2. B'de mcosd durdurulur, mcos-node (Linux yapısı; Windows'takiyle aynı kod)
     anahtarsız başlatılır.
  3. A etkin tarama ile B'yi bulur (bulamazsa elle IP ile dener; ikisi ayrı
     raporlanır).
  4. Kodla eşleştirme: A kod üretir, B aynı kodla kabul eder, A onaylar.
  5. A'da Paper sunucusu kurulur, ortak dünya açılır; B kendi yarısını kurup
     başlatır.
  6. (isteğe bağlı --bot) Ana makinedeki başsız istemci A'ya bağlanır, sınırın
     öbür yanına ışınlanır, aktarım paketini alır ve B'ye bağlanır.

Kullanım: e2e.py [--mc 1.21.11] [--sw paper] [--bot] [--keep]
"""
import argparse, json, os, subprocess, sys, time
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import lan

HERE = lan.HERE
SRC = os.path.dirname(os.path.abspath(__file__))
REPORT = []


def step(name, ok, detail=""):
    REPORT.append((name, ok, detail))
    print(("  OK   " if ok else "  HATA ") + name + ((" — " + detail) if detail else ""), flush=True)


def sh(vm, cmd, t=60, check=False):
    out, code = lan.run(vm, cmd, t)
    if check and code != 0:
        raise RuntimeError(f"{vm}: {cmd} -> {code}\n{out}")
    return out, code


def call(vm, method, params=None, t=60):
    p = "" if params is None else " '" + json.dumps(params).replace("'", "'\\''") + "'"
    out, code = sh(vm, f"mcosctl call {method}{p} 2>&1", t)
    if code != 0:
        return None, out.strip()
    try:
        return json.loads(out), ""
    except Exception:
        return None, out.strip()


def wait(fn, t, every=2.0):
    t0 = time.time()
    while time.time() - t0 < t:
        v = fn()
        if v:
            return v
        time.sleep(every)
    return None


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--mc", default="1.21.11")
    ap.add_argument("--sw", default="paper")
    ap.add_argument("--ram", type=int, default=4096)
    ap.add_argument("--bot", action="store_true")
    ap.add_argument("--keep", action="store_true", help="bitince VM'leri kapatma")
    a = ap.parse_args()

    for vm in ("A", "B"):
        lan.up(vm, a.ram)
    ipA, ipB = lan.VMS["A"][2], lan.VMS["B"][2]
    for vm, ip in (("A", ipA), ("B", ipB)):
        sh(vm, f"ip addr add {ip}/24 dev eth0 && ip link set eth0 up", check=True)
        # Çoğaltma (multicast) LAN kartından çıksın: gerçek evde tek kart var;
        # burada varsayılan yol WAN kartı olduğu için elle yönlendiriliyor.
        sh(vm, "ip route add 224.0.0.0/4 dev eth0 2>/dev/null; true")
    out, code = sh("A", f"ping -c 2 -W 2 {ipB}")
    step("LAN: A → B ping", code == 0, out.strip().splitlines()[-1] if out.strip() else "")

    # ── B: PC düğümü ────────────────────────────────────────────────────────
    sh("B", "/etc/init.d/S99mcos stop >/dev/null 2>&1; killall mcosd 2>/dev/null; sleep 1; true")
    sh("B", "mkdir -p /data/node && (MCOS_NODE_LINK_DIR=/usr/lib/mcos/mods/link mcos-node --data /data/node --kurma --quiet --name PC-B "
            ">/tmp/node.log 2>&1 &) ; sleep 3; tail -3 /tmp/node.log", t=30)
    out, _ = sh("B", "netstat -ltn 2>/dev/null | grep -E ':(2222|27892|25565) ' ; true")
    step("B: mcos-node dinliyor", ":2222" in out or "2222" in out, out.strip().replace("\n", " | "))

    # ── A: PC paylaşımını aç (kullanıcı Ayarlar'dan açar; öntanımlı KAPALI) ─
    res, err = call("A", "config.get")
    cfg = (res or {}).get("config") or {}
    cfg.setdefault("cluster", {})["enabled"] = True
    res, err = call("A", "config.set", cfg)
    step("A: PC paylaşımı açıldı", res is not None, err)
    out, _ = sh("A", "sleep 2; netstat -ltn 2>/dev/null | grep ':2222 ' ; true")
    step("A: eşleştirme portu dinleniyor", "2222" in out, out.strip())

    # ── Keşif ───────────────────────────────────────────────────────────────
    peer = None
    t0 = time.time()
    res, err = call("A", "cluster.scan", None, t=90)
    peers = (res or {}).get("peers") or []
    for p in peers:
        if p.get("ip") == ipB:
            peer = p
    step("Tarama B'yi buldu", peer is not None,
         f"{time.time()-t0:.1f} sn, bulunan: " + ", ".join(f"{p.get('name')}@{p.get('ip')}" for p in peers) + (err and " " + err))
    if peer is None:
        res, err = call("A", "cluster.pairManual", {"address": ipB}, t=60)
        step("Elle IP ile eklendi", bool(res), json.dumps(res)[:200] if res else err)
        res2, _ = call("A", "cluster.peers")
        for p in (res2 or {}).get("peers") or []:
            if p.get("ip") == ipB:
                peer = p
    if peer is None:
        return finish(a)

    # ── Kodla eşleştirme ────────────────────────────────────────────────────
    res, err = call("A", "cluster.pairOffer", {"id": peer["id"]}, t=30)
    code = ((res or {}).get("code", "") or "").replace(" ", "")
    step("A eşleştirme kodu üretti", bool(code) and (res or {}).get("supported"), f"kod {code} {err}")
    if not code:
        return finish(a)
    out, _ = sh("B", f"mcos-node --data /data/node --kabul {code} 2>&1 | tail -3", t=170)
    step("B aynı kodu kabul etti", "kabul" in out.lower() or "tamam" in out.lower(), out.strip().replace("\n", " | "))
    st = wait(lambda: (lambda r: r if r and r[0] and r[0].get("state") == "tamam" else None)(
        call("A", "cluster.pairConfirm", {"id": peer["id"]})), 60)
    step("A onayladı (eşleşti)", st is not None, json.dumps(st[0]) if st else "")
    res, _ = call("A", "cluster.peers")
    pb = [p for p in (res or {}).get("peers") or [] if p.get("id") == peer["id"]]
    step("A: eş listesinde EŞLEŞTİ", bool(pb) and pb[0].get("paired"),
         (pb[0].get("problem") or pb[0].get("state", "")) if pb else "")
    out, _ = sh("B", "cat /data/node/node.json 2>/dev/null | head -c 400")
    step("B: anahtar kaydedildi", '"key": ""' not in out and '"key"' in out, "")

    # ── Ortak dünya ─────────────────────────────────────────────────────────
    res, err = call("A", "server.create", {"name": "Ortak", "software": a.sw, "mcVersion": a.mc,
                                           "ramMB": 1536, "onlineMode": False, "maxPlayers": 20}, t=60)
    srv = (res or {}).get("server") or {}
    sid = srv.get("id", "")
    step("A: sunucu oluşturuldu", bool(sid), err)
    if not sid:
        return finish(a)
    t0 = time.time()
    res, err = call("A", "server.start", {"id": sid}, t=600)
    ok = wait(lambda: (lambda r: r if r and ((r[0] or {}).get("server") or r[0] or {}).get("state") == "running" else None)(
        call("A", "server.get", {"id": sid})), 900, 5)
    step("A: sunucu çalışıyor", ok is not None, f"{time.time()-t0:.0f} sn {err}")
    res, err = call("A", "link.enable", {"serverId": sid}, t=120)
    step("link.enable", res is not None, (json.dumps(res) if res else err)[:300])
    t0 = time.time()
    ls = wait(lambda: (lambda r: r if r and len([n for n in (r.get("nodes") or []) if n.get("online")]) >= 2 else None)(
        call("A", "link.status")[0]), 1200, 10)
    step("İki düğüm de çevrimiçi (ortak dünya)", ls is not None, f"{time.time()-t0:.0f} sn")
    res, _ = call("A", "link.status")
    print(json.dumps(res, indent=1, ensure_ascii=False)[:2500])
    step("A: mod/eklenti kurulu", bool(res) and res.get("modInstalled"), (res or {}).get("note", "") + " " + (res or {}).get("modProblem", ""))
    out, _ = sh("B", "ls /data/node/servers/*/data/plugins /data/node/servers/*/data/mods 2>/dev/null | head; grep -E \"online-mode\" /data/node/servers/*/data/server.properties")
    step("B: eklenti kurulu", "mcos-link" in out, out.strip().replace("\n", " | ")[:300])

    if a.bot and ls:
        bot(a, sid, res)
    return finish(a)


def bot(a, sid, link):
    # Sınırın öbür tarafı: B'nin dilimi (düğüm adı "self" olmayan).
    selfname = [n.get("name") for n in link.get("nodes") or [] if n.get("self")]
    terr = [t for t in link.get("territories") or [] if t.get("node") not in selfname]
    step("B'nin dilimi var", bool(terr), json.dumps(terr)[:200])
    if not terr:
        return
    t = terr[0]
    if t.get("unboundedMin"):
        cx = t["maxChunkX"] - 4
    elif t.get("unboundedMax"):
        cx = t["minChunkX"] + 4
    else:
        cx = (t["minChunkX"] + t["maxChunkX"]) // 2
    x = cx * 16 + 8
    ensure_bot()
    cmd = ["node", os.path.join(SRC, "bot.js"), "--host", "127.0.0.1", "--port", str(lan.VMS["A"][3][0][0]),
           "--version", a.mc, "--map", f"{lan.VMS['B'][2]}:25565=127.0.0.1:{lan.VMS['B'][3][0][0]}",
           "--mcosctl", f"python3 {os.path.join(SRC, 'lan.py')} sh A 'mcosctl cmd {sid} %s'"]
    cmd += ["--x", str(x)]
    p = subprocess.run(cmd, capture_output=True, text=True, timeout=300)
    print(p.stdout[-3000:], p.stderr[-1500:])
    try:
        r = json.loads(p.stdout.strip().splitlines()[-1])
    except Exception:
        r = {}
    step("İstemci A'ya girdi", r.get("joinedA"), "")
    step("Aktarım paketi geldi", bool(r.get("transfer")), json.dumps(r.get("transfer")))
    step("İstemci B'ye girdi", r.get("joinedB"), r.get("error", ""))
    step("Envanter korundu", r.get("inventoryKept"), json.dumps(r.get("inventory"))[:200])


def ensure_bot():
    """minecraft-protocol'ü çalışma klasörüne bir kez kurar (depoya değil)."""
    nm = os.path.join(HERE, "bot", "node_modules", "minecraft-protocol")
    if not os.path.isdir(nm):
        os.makedirs(os.path.join(HERE, "bot"), exist_ok=True)
        subprocess.run("npm init -y >/dev/null && npm install --no-audit --no-fund minecraft-protocol",
                       shell=True, cwd=os.path.join(HERE, "bot"), check=True)
    os.environ["MCOS_LAN_BOT_MODULES"] = os.path.join(HERE, "bot", "node_modules")


def finish(a):
    print("\n══ ÖZET ══")
    for n, ok, d in REPORT:
        print(("OK   " if ok else "HATA ") + n + ((" — " + d) if d else ""))
    with open(os.path.join(HERE, "e2e-report.json"), "w") as f:
        json.dump(REPORT, f, ensure_ascii=False, indent=1)
    if not a.keep:
        for vm in ("A", "B"):
            lan.down(vm)
    return 0 if all(ok for _, ok, _ in REPORT) else 1


if __name__ == "__main__":
    sys.exit(main())
