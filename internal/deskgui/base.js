// MCOS masaüstü arayüzü — ortak yardımcılar.
//
// Belirteç sayfaya <meta> ile gömülür ve HER API isteğinde özel başlıkta
// gönderilir (bkz. server.go GÜVENLİK). Çerez KULLANILMAZ: çerez tarayıcı
// tarafından başka sitelerin isteklerine de otomatik eklenirdi.
"use strict";

const BELIRTEC = document.querySelector('meta[name="mcos-belirtec"]').content;

// api: GET (govde yoksa) ya da POST (govde varsa) yapar, JSON döner.
// Sunucu {hata: "..."} ile yanıt verirse Error fırlatılır; ileti Türkçedir
// ve kullanıcıya olduğu gibi gösterilebilir.
async function api(yol, govde) {
  const secenek = { method: govde === undefined ? "GET" : "POST", headers: { "X-MCOS-Belirtec": BELIRTEC } };
  if (govde !== undefined) {
    secenek.headers["Content-Type"] = "application/json";
    secenek.body = JSON.stringify(govde);
  }
  let yanit;
  try {
    yanit = await fetch(yol, secenek);
  } catch (e) {
    baglantiKoptu();
    throw new Error("uygulamaya ulaşılamıyor");
  }
  baglantiVar();
  const veri = await yanit.json().catch(() => ({}));
  if (!yanit.ok) throw new Error(veri.hata || ("HTTP " + yanit.status));
  return veri;
}

// Kopan bağlantı: program kapandıysa sayfa donmuş görünmesin, söylesin.
let kopukSayac = 0;
function baglantiKoptu() {
  kopukSayac++;
  if (kopukSayac < 3) return;
  let k = document.getElementById("koptu");
  if (!k) {
    k = document.createElement("div");
    k.id = "koptu";
    k.innerHTML = '<div class="kart"><h3>Uygulama kapandı</h3>' +
      '<p class="dim">Bu pencerenin bağlı olduğu program artık çalışmıyor. ' +
      'Programı yeniden açın.</p></div>';
    document.body.appendChild(k);
  }
  k.classList.add("acik");
}
function baglantiVar() {
  kopukSayac = 0;
  const k = document.getElementById("koptu");
  if (k) k.classList.remove("acik");
}

// esc: metni HTML'e güvenle koyar. Aygıt modelleri, sunucu adları ve eş
// adları dışarıdan gelir; innerHTML'e ham basmak betik enjeksiyonu olurdu.
function esc(s) {
  return String(s == null ? "" : s)
    .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;").replace(/'/g, "&#39;");
}

function $(id) { return document.getElementById(id); }

// bayt: 1536 -> "1.5 KB" (flash.HumanBytes ile aynı birimler).
function bayt(n) {
  if (!n) return "0 B";
  const b = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (n >= 1024 && i < b.length - 1) { n /= 1024; i++; }
  return (i === 0 ? n : n.toFixed(1)) + " " + b[i];
}

// bildir: sayfanın üstünde kısa bir ileti (başarı ya da hata).
function bildir(metin, tur) {
  let b = $("bildirim");
  if (!b) {
    b = document.createElement("div");
    b.id = "bildirim";
    b.style.cssText = "position:fixed;left:50%;bottom:18px;transform:translateX(-50%);z-index:20;" +
      "max-width:80%;padding:9px 14px;border-radius:8px;font-weight:600;box-shadow:0 6px 24px #0008;" +
      "transition:opacity .2s";
    document.body.appendChild(b);
  }
  const renk = { hata: "var(--error)", uyari: "var(--warn)", ok: "var(--ok)" }[tur] || "var(--accent)";
  b.style.background = "var(--raised)";
  b.style.border = "1px solid " + renk;
  b.style.color = renk;
  b.textContent = metin;
  b.style.opacity = "1";
  clearTimeout(bildir.t);
  bildir.t = setTimeout(() => { b.style.opacity = "0"; }, tur === "hata" ? 6000 : 3000);
}

// dongu: fn'i aralıkla çağırır; biri bitmeden ötekini başlatmaz (yavaş bir
// yanıt, istekleri üst üste yığmasın).
function dongu(fn, aralik) {
  let calisiyor = false;
  const tik = async () => {
    if (calisiyor) return;
    calisiyor = true;
    try { await fn(); } catch (e) { /* bildirim fn'in işi */ }
    calisiyor = false;
  };
  tik();
  return setInterval(tik, aralik);
}
