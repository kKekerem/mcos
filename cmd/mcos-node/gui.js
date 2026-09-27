// MCOS Düğüm penceresi.
//
// Sayfa hiçbir durumu kendisi TUTMAZ: her 1 saniyede /api/durum okunur ve
// ekran baştan çizilir. Düğüm arka planda ayrı bir süreçtir; pencere kapanıp
// açılsa bile doğru durumu göstermenin tek yolu budur.
//
// İstisna eşleştirme kartıdır: kart yalnızca teklif listesi DEĞİŞİNCE
// yeniden kurulur, geri sayım yerinde güncellenir. Her turda innerHTML ile
// baştan kurulsaydı "Kabul et"e basılırken düğme altından değişir ve tık
// kaybolurdu.
"use strict";

let son = null;          // son okunan durum
let adDolduruldu = false;
let teklifImza = "";     // çizili teklif kartlarının özeti
const bekleyenKarar = {}; // id -> true: düğmeye basıldı, yanıt bekleniyor

const DURUM_ETIKET = {
  running: ["Çalışıyor", "ok"],
  starting: ["Başlıyor", "uyari"],
  stopping: ["Duruyor", "uyari"],
  stopped: ["Durdu", ""],
  error: ["Hata", "hata"],
};

// ozet: pencerenin üstündeki büyük durum satırı ve başlık rozeti.
function ozet(d) {
  const n = d.dugum || {};
  const teklif = (n.offers || []).length;
  const esler = n.peerList || [];
  const cevrimici = esler.filter((p) => p.online);
  if (d.islem) return ["vurgu", "…", d.islem, "Lütfen bekleyin.", "İşlem sürüyor"];
  if (!d.calisiyor) {
    return ["", "!", "Düğüm çalışmıyor",
      "MCOS bu PC'yi göremez. Aşağıdan <b>Başlat</b>'a basın.", "Durdu"];
  }
  if (teklif) {
    return ["vurgu", "?", "Eşleştirme isteği var",
      "Yukarıdaki kodu MCOS ekranındakiyle karşılaştırın.", "Eşleştirme isteği"];
  }
  const olay = n.pairEvent;
  if (!esler.length && n.keySet && olay && olay.ok && Date.now() - Date.parse(olay.at) < 60000) {
    // Anahtar geldi; MCOS bağlantıyı anahtarla doğrulayıp kendini bu PC'ye
    // kaydedene kadar (birkaç saniye) "bekleniyor" demek yanıltıcıydı.
    return ["vurgu", "…", "Eşleşme tamamlanıyor",
      "Anahtar alındı; MCOS bağlantıyı doğruluyor…", "Eşleşiyor"];
  }
  if (cevrimici.length) {
    return ["ok", "✓", "Eşleşti — " + esc(cevrimici.map((p) => p.name).join(", ")),
      "Hazır. MCOS'ta ortak dünya açıldığında sunucu bu PC'ye kendiliğinden kurulur.", "Eşleşti"];
  }
  if (esler.length) {
    return ["uyari", "!", "Eşleşmiş MCOS şu an çevrimdışı",
      "MCOS açılınca bağlantı kendiliğinden kurulur.", "MCOS çevrimdışı"];
  }
  return ["vurgu", "…", "Eşleşme bekleniyor",
    "MCOS'ta <b>MCOS Paylaşım → Ağı tara</b> → <b>" + esc(d.ad) + "</b> → <b>Eşleştir</b>. " +
    "Kod burada çıkacak; anahtar girmeniz gerekmez.", "Eşleşme bekleniyor"];
}

function kutu(tur, html) {
  return '<div class="kutu ' + tur + '"><div class="ic">' + html + "</div></div>";
}

function sure(sn) {
  sn = Math.max(0, sn | 0);
  return Math.floor(sn / 60) + ":" + String(sn % 60).padStart(2, "0");
}

// ── Eşleştirme istekleri ──
function teklifleriCiz(teklifler) {
  const imza = teklifler.map((t) => t.id + "/" + t.code + "/" + t.accepted).join(";");
  if (imza !== teklifImza) {
    teklifImza = imza;
    $("teklifler").innerHTML = teklifler.map((t) => {
      const kabul = t.accepted;
      return '<section class="teklif' + (kabul ? " kabul" : "") + '" data-id="' + esc(t.id) + '">' +
        '<div class="ust-satir"><span class="nokta ' + (kabul ? "ok" : "vurgu") + ' nabiz"></span>' +
        (kabul ? "Kabul edildi — MCOS'ta onay bekleniyor" : "Eşleştirme isteği") +
        '<span class="sure" data-sure></span></div>' +
        '<div class="kim">' + esc(t.name) + ' <span class="ip">' + esc(t.ip) + "</span></div>" +
        '<div class="soluk">bu PC ile eşleşmek istiyor</div>' +
        '<div class="kod">' + esc(t.code) + "</div>" +
        (kabul
          ? '<div class="soru">Şimdi MCOS ekranında <b>Kodlar aynı, onayla</b>\'ya basın. Anahtar otomatik gelecek.</div>'
          : '<div class="soru">MCOS ekranında da <b>aynı kod</b> görünüyor mu? Farklıysa reddedin: araya başka biri girmiş olabilir.</div>' +
            '<div class="dugmeler"><button class="birincil" data-kabul="1">Kabul et</button>' +
            '<button data-kabul="0">Reddet</button></div>') +
        "</section>";
    }).join("");
    for (const kart of $("teklifler").querySelectorAll(".teklif")) {
      for (const b of kart.querySelectorAll("button[data-kabul]")) {
        b.onclick = () => karar(kart.dataset.id, b.dataset.kabul === "1");
      }
    }
  }
  // Geri sayım ve düğme durumu yerinde güncellenir.
  for (const t of teklifler) {
    const kart = $("teklifler").querySelector('.teklif[data-id="' + CSS.escape(t.id) + '"]');
    if (!kart) continue;
    kart.querySelector("[data-sure]").textContent = sure(t.leftSec) + " içinde yanıtlanmazsa düşer";
    for (const b of kart.querySelectorAll("button[data-kabul]")) b.disabled = !!bekleyenKarar[t.id];
  }
}

async function karar(id, kabul) {
  const t = ((son && son.dugum && son.dugum.offers) || []).find((x) => x.id === id);
  if (!t) return;
  bekleyenKarar[id] = true;
  if (son) teklifleriCiz(son.dugum.offers || []);
  try {
    const r = await api("/api/karar", { id: id, kod: t.code, kabul: kabul });
    bildir(r.ileti || "Tamam", "ok");
  } catch (e) {
    bildir(e.message, "hata");
  }
  delete bekleyenKarar[id];
  yenile();
}

function ciz(d) {
  son = d;
  const n = d.dugum || {};

  // ── Başlık rozeti ve büyük durum satırı ──
  const [tur, simge, baslik, aciklama, rozetMetin] = ozet(d);
  $("rozet").className = "rozet " + tur;
  $("rozetNokta").className = "nokta " + tur + (tur === "vurgu" ? " nabiz" : "");
  $("rozetMetin").textContent = rozetMetin;
  $("durum").className = "durum " + tur;
  $("durumSimge").textContent = simge;
  $("durumBaslik").innerHTML = baslik;
  $("durumAciklama").innerHTML = aciklama;

  teklifleriCiz(d.calisiyor ? (n.offers || []) : []);

  // ── Bilgi kutuları ──
  let k = "";
  if (d.hata) k += kutu("hata", "<b>Olmadı:</b> " + esc(d.hata));
  if (d.bilgi && !d.islem) k += kutu("vurgu", esc(d.bilgi));
  const olay = n.pairEvent;
  if (olay && Date.now() - Date.parse(olay.at) < 60000) {
    k += kutu(olay.ok ? "vurgu" : "uyari", esc(olay.msg));
  }
  if (d.gomulu) k += kutu("uyari", "<b>Düğüm bu pencerede çalışıyor.</b> Arka planda başlatılamadı; pencereyi kapatınca düğüm ve sunucu durur.");
  if (d.surumFarki) {
    k += kutu("uyari", "Arka plandaki düğüm <b>" + esc(d.surumFarki) + "</b> sürümünde, bu program <b>" +
      esc(d.surum) + '</b>. <button class="kucuk" id="guncelle" style="margin-left:6px">Güncelle ve yeniden başlat</button>');
  }
  $("kutular").innerHTML = k;
  const g = $("guncelle");
  if (g) g.onclick = () => eylem("/api/guncelle", {}, "Güncelleniyor…");

  // ── Nasıl eşleşilir (eşleştikten sonra gereksiz yer kaplamasın) ──
  const esler = n.peerList || [];
  $("nasilKart").style.display = esler.length && !(n.offers || []).length ? "none" : "";
  $("adimAd").textContent = d.ad || "bu PC";
  $("ipIpucu").textContent = d.adres || "—";

  // ── Arka plan servisi ──
  $("pcAd").textContent = d.ad || "—";
  $("pcAdres").textContent = (d.adres || "adres yok") + ":" + d.port;
  $("pcNot").textContent = n.note || (d.calisiyor ? "çalışıyor" : "Düğüm çalışmıyor");
  $("pcSurum").textContent = d.surum;
  $("baslat").disabled = !!(d.calisiyor || d.islem);
  $("durdur").disabled = !!(!d.calisiyor || d.islem);
  const o = d.otobaslat || {};
  $("otobaslat").checked = !!o.acik;
  $("otobaslat").disabled = !o.destek || d.tasinabilir || !!d.islem;
  $("otoNot").textContent = o.not || (o.acik ? "Oturum açıldığında düğüm penceresiz başlar." : "Düğüm yalnızca bu programı açınca başlar.");

  // ── Eşler ──
  $("esSay").textContent = esler.length ? "(" + esler.length + ")" : "";
  if (!esler.length) {
    $("esler").innerHTML = '<div class="bos">' + (d.calisiyor
      ? "Henüz eşleşen MCOS yok."
      : "Düğüm çalışmıyor; MCOS bu PC'yi göremez.") + "</div>";
  } else {
    $("esler").innerHTML = esler.map((p) =>
      '<div class="satir"><span class="nokta ' + (p.online ? "ok" : "") + '"></span>' +
      '<div style="min-width:0"><div class="ad">' + esc(p.name) + "</div>" +
      '<div class="bilgi mono">' + esc(p.ip) + (p.version ? " · " + esc(p.version) : "") + "</div>" +
      (p.problem ? '<div class="bilgi uyari-metin">' + esc(p.problem) + "</div>" : "") +
      '</div><div class="sag ' + (p.online ? "ok-metin" : "soluk") + '">' +
      (p.online ? "çevrimiçi" : "çevrimdışı") + "</div></div>").join("");
  }

  // ── Sunucular ──
  const srv = n.servers || [];
  $("srvSay").textContent = srv.length ? "(" + srv.length + ")" : "";
  if (!srv.length) {
    $("sunucular").innerHTML = '<div class="bos">Bu PC\'de henüz sunucu yok. MCOS\'ta ortak dünya açıldığında ' +
      "sunucu buraya kendiliğinden kurulur; durumu, portu ve bölgesi burada görünür.</div>";
  } else {
    $("sunucular").innerHTML = '<table class="tablo"><thead><tr><th>Sunucu</th><th>Yazılım</th><th>Durum</th>' +
      "<th>Port</th><th>Oyuncu</th><th>Bölge</th></tr></thead><tbody>" +
      srv.map((s) => {
        const [et, cl] = DURUM_ETIKET[s.state] || [s.state, ""];
        return "<tr><td><b>" + esc(s.name) + "</b>" + (s.shared ? ' <span class="rozet vurgu" style="padding:1px 7px;font-size:11px">ortak dünya</span>' : "") +
          '</td><td class="dim">' + esc(s.software) + " " + esc(s.mcVersion) +
          '</td><td><span class="nokta ' + cl + '"></span> ' + esc(et) +
          '</td><td class="mono">' + esc(s.port) +
          "</td><td>" + esc(s.players) + (s.maxPlayers ? " / " + esc(s.maxPlayers) : "") +
          '</td><td class="mono">' + esc(s.region || "—") + "</td></tr>";
      }).join("") + "</tbody></table>";
  }

  // ── Gelişmiş: elle anahtar ──
  $("anahtarDurum").style.display = d.anahtarVar ? "" : "none";
  $("anahtarIpucu").textContent = d.anahtarIpucu || "";
  if (!adDolduruldu && d.ad) {
    $("ad").value = d.ad;
    adDolduruldu = true;
  }

  $("gunlukYol").textContent = d.gunluk || "";
  $("altNot").textContent = d.gomulu
    ? "Düğüm bu pencerede çalışıyor: pencereyi kapatınca durur."
    : "Bu pencereyi kapatmak düğümü durdurmaz: düğüm arka planda çalışmaya devam eder.";
}

async function yenile() {
  try {
    ciz(await api("/api/durum"));
  } catch (e) { /* bağlantı katmanı base.js'te */ }
}

async function gunlukYenile() {
  const r = await api("/api/gunluk");
  const pre = $("gunluk");
  const dipte = pre.scrollHeight - pre.scrollTop - pre.clientHeight < 30;
  const satirlar = r.satirlar || [];
  pre.textContent = satirlar.length ? satirlar.join("\n") : "Günlük henüz boş.";
  if (dipte) pre.scrollTop = pre.scrollHeight;
}

async function eylem(yol, govde, bekleme) {
  try {
    const r = await api(yol, govde);
    bildir(r.ileti || bekleme || "Tamam", "ok");
  } catch (e) {
    bildir(e.message, "hata");
  }
  yenile();
}

$("baslat").onclick = () => eylem("/api/baslat", {}, "Başlatılıyor…");
$("durdur").onclick = () => eylem("/api/durdur", {}, "Durduruluyor…");
$("klasor").onclick = () => eylem("/api/klasor", {}, "Klasör açılıyor…");

$("otobaslat").onchange = async (ev) => {
  const istek = ev.target.checked;
  try {
    await api("/api/otobaslat", { acik: istek });
    bildir(istek ? "Oturum açılışında başlayacak." : "Oturum açılışında başlamayacak.", "ok");
  } catch (e) {
    ev.target.checked = !istek;
    bildir(e.message, "hata");
  }
  yenile();
};

$("anahtarForm").onsubmit = async (ev) => {
  ev.preventDefault();
  const anahtar = $("anahtar").value.trim();
  const ad = $("ad").value.trim();
  $("kaydet").disabled = true;
  try {
    const r = await api("/api/anahtar", { anahtar: anahtar, ad: ad });
    bildir(r.ileti || "Kaydedildi.", "ok");
    $("anahtar").value = "";
  } catch (e) {
    bildir(e.message, "hata");
  }
  $("kaydet").disabled = false;
  yenile();
};

$("kopyala").onclick = async () => {
  const metin = $("ipIpucu").textContent;
  try {
    await navigator.clipboard.writeText(metin);
    bildir("Adres kopyalandı: " + metin, "ok");
  } catch (e) {
    bildir("Kopyalanamadı; adres: " + metin, "uyari");
  }
};

dongu(yenile, 1000);
dongu(gunlukYenile, 3000);
