// Başsız Minecraft istemcisi: ortak dünya aktarımını GERÇEK protokolle sınar.
//
// A'ya girer, A'nın konsolundan eşya verilip B'nin dilimine ışınlanır,
// sunucunun gönderdiği "transfer" paketini yakalar, B'ye aktarım amacıyla
// (el sıkışma nextState=3, gerçek istemcinin yaptığı gibi) bağlanır ve
// envanterin geldiğini denetler. Son satır JSON sonuçtur.
const path = require('path')
const { execSync } = require('child_process')
const mc = require(path.join(process.env.MCOS_LAN_BOT_MODULES || path.join(__dirname, 'node_modules'), 'minecraft-protocol'))

const args = {}
for (let i = 2; i < process.argv.length; i += 2) args[process.argv[i].slice(2)] = process.argv[i + 1]
const version = args.version || '1.21.11'
const name = 'Gezgin'
const res = { joinedA: false, transfer: null, joinedB: false, inventoryKept: false, inventory: null }
const map = {}
for (const m of (args.map || '').split(',').filter(Boolean)) {
  const [from, to] = m.split('=')
  map[from] = to
}

function done (err) {
  if (err) res.error = String(err)
  console.log(JSON.stringify(res))
  process.exit(0)
}
setTimeout(() => done('zaman aşımı'), 240000)

function konsol (cmd) {
  try {
    execSync(args.mcosctl.replace('%s', cmd), { stdio: 'pipe', timeout: 30000 })
  } catch (e) { /* çıktı önemli değil */ }
}

function baglan (host, port, transfer) {
  const c = mc.createClient({ host, port: +port, username: name, version, auth: 'offline', hideErrors: true })
  // Gerçek istemci yapılandırma/oyun "ping"lerine "pong" ile cevap verir.
  // Fabric API yapılandırma aşamasında kanal eşitlemesi için ping atıp pong
  // bekliyor; cevap gelmezse giriş orada takılıyordu (ilk koşuda görüldü).
  c.on('ping', (p) => { try { c.write('pong', { id: p.id }) } catch (e) {} })
  if (transfer) {
    const w = c.write.bind(c)
    c.write = (n, p) => { if (n === 'set_protocol') p.nextState = 3; return w(n, p) }
  }
  return c
}

function envanter (c, cb) {
  // Oyuncu envanteri pencere 0'dır; sunucu girişte window_items gönderir.
  c.on('window_items', (p) => {
    if (p.windowId !== 0) return
    const items = (p.items || []).filter(it => it && (it.itemCount || it.present) && (it.itemId !== undefined || it.item !== undefined))
    cb(items)
  })
}

const a = baglan(args.host, args.port, false)
a.on('error', e => done('A: ' + e.message))
a.on('kick_disconnect', p => done('A attı: ' + JSON.stringify(p)))
a.on('login', () => {
  res.joinedA = true
  setTimeout(() => {
    konsol(`give ${name} minecraft:diamond 5`)
    setTimeout(() => konsol(`tp ${name} ${args.x} 120 0`), 1500)
  }, 3000)
})
a.on('position', (p) => {
  // Işınlanma onayı: gerçek istemci her konumlandırmayı onaylar.
  if (p.teleportId !== undefined) a.write('teleport_confirm', { teleportId: p.teleportId })
})
a.on('transfer', (p) => {
  res.transfer = { host: p.host, port: p.port }
  const key = `${p.host}:${p.port}`
  const to = map[key] || key
  const [h, pt] = to.split(':')
  a.end()
  const b = baglan(h, pt, true)
  b.on('error', e => done('B: ' + e.message))
  b.on('kick_disconnect', p2 => done('B attı: ' + JSON.stringify(p2)))
  b.on('disconnect', p2 => done('B attı (yapılandırma): ' + JSON.stringify(p2)))
  b.on('login', () => { res.joinedB = true })
  envanter(b, (items) => {
    res.inventory = items.map(it => ({ id: it.itemId ?? it.item, n: it.itemCount }))
    res.inventoryKept = items.some(it => (it.itemCount || 0) >= 5)
    setTimeout(() => { b.end(); done() }, 1000)
  })
})
