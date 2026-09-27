// MCOS USB Kurucu penceresi.
//
// Seçimler (imaj, aygıt) sayfada tutulur ama YAZMA kararını sunucu verir:
// onay metni, aygıtın hâlâ aynı boyutta olduğu ve imajın listede bulunduğu
// orada yeniden denetlenir (gui.go handleWrite). Buradaki düğme kilitleri
// yalnızca kullanıcıya kolaylıktır, güvenlik değildir.
"use strict";

let durum = null;
let imajlar = [];
let aygitlar = [];
let secImaj = null;   // yol
let secAygit = null;  // {yol, boyut}
let yaziyor = false;

function kutu(tur, html) {
  return '<div class="kutu ' + tur + '"><div class="ic">' + html + "</div></div>";
}

function kutulariCiz() {
  if (!durum) return;
  let k = "";
  if (durum.deneme) k += kutu("vurgu", "<b>DENEME KİPİ</b> — hiçbir aygıta yazılmayacak; akış baştan sona sınanır.");
  if (!durum.yonetici && !durum.deneme) {
    k += kutu("uyari", "<b>Yönetici hakkı yok.</b> Aygıtları görebilirsiniz ama USB'ye yazmak için yönetici hakkı gerekir." +
      (durum.windows ? ' <button class="kucuk" id="yonetici" style="margin-left:6px">Yönetici olarak yeniden aç</button>'
                     : " Kök hakkıyla başlatın: <span class=\"mono\">sudo ./mcos-flash.sh --gui</span>"));
  }
  $("kutular").innerHTML = k;
  const y = $("yonetici");
  if (y) y.onclick = async () => {
    try { const r = await api("/api/yonetici", {}); bildir(r.ileti || "Açılıyor…", "ok"); }
    catch (e) { bildir(e.message, "hata"); }
  };
}

function imajlariCiz(r) {
  imajlar = r.imajlar || [];
  $("imSay").textContent = imajlar.length ? "(" + imajlar.length + ")" : "";
  if (!imajlar.length) {
    secImaj = null;
    $("imajlar").innerHTML = '<div class="bos">Hiç imaj bulunamadı. <b>.img</b> dosyasını bu programın yanına koyup <b>Yenile</b>\'ye basın.' +
      '<div class="soluk mono" style="margin-top:8px;font-size:12px">' + (r.klasorler || []).map(esc).join("<br>") + "</div></div>";
    ozetCiz();
    return;
  }
  if (!secImaj || !imajlar.some((i) => i.yol === secImaj)) secImaj = imajlar[0].yol; // en yeni önerilir
  $("imajlar").innerHTML = imajlar.map((im) => {
    const rozet = im.enYeni ? '<span class="rozet vurgu">EN YENİ</span>'
      : (im.bayat ? '<span class="rozet uyari">ESKİ — ' + esc(im.bayat) + " önce</span>" : "");
    return '<div class="satir secilebilir' + (im.yol === secImaj ? " secili" : "") + '" data-yol="' + esc(im.yol) + '">' +
      '<span class="nokta ' + (im.yol === secImaj ? "vurgu" : "") + '"></span>' +
      '<div style="min-width:0"><div class="ad">' + esc(im.ad) + '</div><div class="bilgi mono">' + esc(im.yol) + "</div></div>" +
      '<div class="sag">' + rozet + '<div class="bilgi">' + bayt(im.boyut) + " · " + esc(im.tarih) + "</div></div></div>";
  }).join("");
  document.querySelectorAll("#imajlar .satir").forEach((el) => {
    el.onclick = () => { if (yaziyor) return; secImaj = el.dataset.yol; imajlariCiz({ imajlar: imajlar }); };
  });
  ozetCiz();
}

function aygitlariCiz(r) {
  aygitlar = r.aygitlar || [];
  $("aySay").textContent = aygitlar.length ? "(" + aygitlar.length + ")" : "";
  if (secAygit && !aygitlar.some((a) => a.yol === secAygit.yol && a.boyut === secAygit.boyut)) {
    // Seçilen bellek çıkarıldı ya da yerine başkası takıldı: seçim ve onay
    // SIFIRLANIR — eski onay yeni diske geçmemeli.
    secAygit = null;
    $("onay").value = "";
  }
  if (!aygitlar.length) {
    $("aygitlar").innerHTML = (r.hata ? kutu("hata", esc(r.hata)) : "") +
      '<div class="bos">USB bellek bulunamadı. Belleği takın; liste birkaç saniyede kendiliğinden yenilenir.</div>';
    ozetCiz();
    return;
  }
  $("aygitlar").innerHTML = aygitlar.map((a) => {
    const secili = secAygit && secAygit.yol === a.yol;
    const uyarilar = (a.uyarilar || []).map((u) => '<div class="bilgi uyari-metin">! ' + esc(u) + "</div>").join("");
    return '<div class="satir secilebilir' + (secili ? " secili" : "") + (a.kucuk ? " pasif" : "") +
      '" data-yol="' + esc(a.yol) + '" data-boyut="' + a.boyut + '">' +
      '<span class="nokta ' + (secili ? "vurgu" : "") + '"></span>' +
      '<div style="min-width:0"><div class="ad">' + esc(a.model || a.ad) + '</div><div class="bilgi mono">' + esc(a.yol) +
      (a.veriyolu ? " · " + esc(a.veriyolu) : "") + "</div>" + uyarilar +
      (a.kucuk ? '<div class="bilgi hata-metin">Kalıcı kurulum için çok küçük (en az ' + bayt(durum ? durum.enKucuk : 0) + ")</div>" : "") +
      '</div><div class="sag buyuk">' + bayt(a.boyut) + "</div></div>";
  }).join("");
  document.querySelectorAll("#aygitlar .satir").forEach((el) => {
    el.onclick = () => {
      if (yaziyor || el.classList.contains("pasif")) return;
      const yeni = { yol: el.dataset.yol, boyut: Number(el.dataset.boyut) };
      if (!secAygit || secAygit.yol !== yeni.yol) $("onay").value = "";
      secAygit = yeni;
      aygitlariCiz({ aygitlar: aygitlar });
      $("onay").focus();
    };
  });
  ozetCiz();
}

function ozetCiz() {
  const a = secAygit && aygitlar.find((x) => x.yol === secAygit.yol);
  const im = secImaj && imajlar.find((x) => x.yol === secImaj);
  $("ozetAygit").innerHTML = a ? '<span class="mono">' + esc(a.yol) + "</span> · " + bayt(a.boyut) + " · " + esc(a.model || "") : '<span class="soluk">aygıt seçilmedi</span>';
  $("ozetImaj").innerHTML = im ? esc(im.ad) + " · " + bayt(im.boyut) + (im.bayat ? ' <span class="uyari-metin">(ESKİ — en yenisinden ' + esc(im.bayat) + " önce)</span>" : "") : '<span class="soluk">imaj seçilmedi</span>';
  if (durum) $("ozetDogrula").textContent = durum.deneme ? "deneme kipi — yazılmaz" : (durum.dogrulama ? "açık — yazdıktan sonra geri okunup karşılaştırılır" : "KAPALI (--no-verify)");
  $("onayYol").textContent = a ? a.yol : "—";
  $("onay").disabled = !a || yaziyor;
  $("silUyari").style.display = durum && durum.deneme ? "none" : "";
  let neden = "";
  if (!im) neden = "Önce bir imaj seçin.";
  else if (!a) neden = "Önce hedef belleği seçin.";
  else if ($("onay").value.trim() !== a.yol) neden = "Onay alanına aygıt yolunu yazın.";
  else if (durum && !durum.yonetici && !durum.deneme) neden = "Yönetici hakkı gerekli.";
  else if (im.boyut > a.boyut) neden = "İmaj bu belleğe sığmıyor.";
  if (yaziyor) neden = "Yazma sürüyor…";
  $("yaz").disabled = neden !== "";
  $("yaz").textContent = durum && durum.deneme ? "Deneme yazması başlat" : "USB'ye yaz";
  $("yazNeden").textContent = neden;
}

function ilerlemeCiz(p) {
  if (!p || (!p.etkin && !p.basari && !p.hata)) { $("ilerlemeKart").style.display = "none"; return; }
  $("ilerlemeKart").style.display = "";
  $("asama").textContent = p.etkin ? (p.asama || "…") : (p.basari ? "Tamamlandı" : "Olmadı");
  $("yuzde").textContent = p.yuzde + "%";
  let ayr = bayt(p.yazilan) + " / " + bayt(p.toplam);
  if (p.etkin && p.hiz > 0) ayr += " · " + bayt(p.hiz) + "/s";
  if (p.etkin && p.kalan) ayr += " · kalan " + p.kalan;
  $("ayrinti").textContent = ayr;
  $("cubuk").style.width = p.yuzde + "%";
  $("cubuk").style.background = p.hata ? "var(--error)" : (p.basari ? "var(--ok)" : "var(--accent)");
  $("iptal").style.display = p.etkin ? "" : "none";
  if (p.basari) {
    $("sonuc").innerHTML = kutu("vurgu", "<b>Tamamlandı</b>" + (p.dogrulama ? " ve doğrulandı" : "") + " — " + esc(p.sure) + "." +
      (p.deneme ? " (DENEME kipiydi: hiçbir aygıta yazılmadı.)" : "<br>Belleği çıkarmadan önce birkaç saniye bekleyin. Açılış için BIOS/UEFI'de USB'yi ilk sıraya alın."));
  } else if (p.hata) {
    $("sonuc").innerHTML = kutu("hata", "<b>Olmadı:</b> " + esc(p.hata));
  } else {
    $("sonuc").innerHTML = "";
  }
}

async function durumYenile() {
  durum = await api("/api/durum");
  $("surum").textContent = durum.surum;
  const oncekiYaziyor = yaziyor;
  yaziyor = !!(durum.ilerleme && durum.ilerleme.etkin);
  kutulariCiz();
  ilerlemeCiz(durum.ilerleme);
  ozetCiz();
  if (oncekiYaziyor && !yaziyor) aygitYenile();
}

async function imajYenile() { imajlariCiz(await api("/api/imajlar")); }
async function aygitYenile() { if (!yaziyor) aygitlariCiz(await api("/api/aygitlar")); }

$("onay").oninput = ozetCiz;
$("imYenile").onclick = () => imajYenile().catch((e) => bildir(e.message, "hata"));
$("ayYenile").onclick = () => aygitYenile().catch((e) => bildir(e.message, "hata"));
$("iptal").onclick = async () => {
  try { const r = await api("/api/iptal", {}); bildir(r.ileti, "uyari"); } catch (e) { bildir(e.message, "hata"); }
};
$("yaz").onclick = async () => {
  if (!secAygit || !secImaj) return;
  $("yaz").disabled = true;
  try {
    await api("/api/yaz", { imaj: secImaj, aygit: secAygit.yol, boyut: secAygit.boyut, onay: $("onay").value });
    bildir("Yazma başladı.", "ok");
    $("onay").value = "";
  } catch (e) {
    bildir(e.message, "hata");
  }
  durumYenile().catch(() => {});
};

dongu(durumYenile, 1000);
imajYenile().catch(() => {});
dongu(aygitYenile, 3000);
