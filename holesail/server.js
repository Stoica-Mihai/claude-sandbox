// Control API for the Holesail share tunnel.
//
// JSON contract (all responses application/json):
//   GET  /healthz               -> 200 {"ok":true}
//   GET  /api/share/status      -> 200 {state, url, error}
//   POST /api/share/start       -> 200 status | 502 status (state:"error")
//   POST /api/share/stop        -> 200 status
//   POST /api/share/regenerate  -> 200 status | 502 status (state:"error")
//   state: "private" | "publishing" | "public" | "error"
//   url:   "hs://s000<key>" when public, else null
//
// The status shape is the ShareStatus/ShareState contract in shared/types.go —
// the frontend's tunnel-guard 403 marshals the same type; keep them in sync.
//
// Boot state is always private — public is never restored across restarts.

const http = require('http')
const net = require('net')
const fs = require('fs')
const path = require('path')
const crypto = require('crypto')
const Holesail = require('holesail')

const TARGET_HOST = process.env.SHARE_TARGET_HOST || 'frontend'
// Default matches the frontend's TUNNEL LISTENER (8090), not the dashboard
// port: which socket tunnel traffic lands on is the share-origin security
// model, so the safe default must be the tunnel side.
const TARGET_PORT = Number(process.env.SHARE_TARGET_PORT || 8090)
const CONTROL_PORT = Number(process.env.SHARE_CONTROL_PORT || 9000)
// Holesail publishes its target host/port to the DHT, and the CLIENT uses that
// host as its own LOCAL bind address (holesail-client reads dhtData.host). So
// the target host must be a loopback every client can bind — 127.0.0.1 — not
// "frontend". We point Holesail at a loopback relay here and forward that to
// the real frontend, so the published host stays 127.0.0.1.
const RELAY_PORT = Number(process.env.SHARE_RELAY_PORT || 8080)
const KEY_FILE = process.env.SHARE_KEY_FILE || '/data/share.key'
// Must stay under the frontend proxy's 30s client timeout so failures
// surface as this wrapper's JSON body, not a bare proxy 502.
const READY_TIMEOUT_MS = 25000

// Load the persisted key, or create one. The key IS the credential:
// the secure connection string is literally "hs://s000" + key.
function loadOrCreateKey(file) {
  try {
    const key = fs.readFileSync(file, 'utf8').trim()
    if (/^[0-9a-f]{64}$/.test(key)) return key
  } catch {}
  const key = crypto.randomBytes(32).toString('hex')
  fs.mkdirSync(path.dirname(file), { recursive: true })
  fs.writeFileSync(file, key + '\n', { mode: 0o600 })
  return key
}

// Loopback relay: Holesail forwards tunnel streams to 127.0.0.1:RELAY_PORT,
// which we pipe to the real dashboard at frontend:8080.
const relay = net.createServer((client) => {
  const upstream = net.connect(TARGET_PORT, TARGET_HOST)
  client.on('error', () => upstream.destroy())
  upstream.on('error', () => client.destroy())
  client.pipe(upstream)
  upstream.pipe(client)
})
relay.on('error', (err) => {
  console.error(`relay listener failed on :${RELAY_PORT}: ${err.message}`)
  process.exit(1)
})
relay.listen(RELAY_PORT, '127.0.0.1')

let key = loadOrCreateKey(KEY_FILE)
let hs = null
let state = 'private'
let lastError = null
// Serializes start/stop/regenerate so concurrent toggles can't race.
let op = Promise.resolve()

function status() {
  return {
    state,
    url: state === 'public' ? 'hs://s000' + key : null,
    error: state === 'error' ? lastError : null
  }
}

function withTimeout(promise, ms, msg) {
  let timer
  const timeout = new Promise((_, reject) => {
    timer = setTimeout(() => reject(new Error(msg)), ms)
  })
  return Promise.race([promise, timeout]).finally(() => clearTimeout(timer))
}

async function closeInstance() {
  if (!hs) return
  try {
    await hs.close()
  } catch {}
  hs = null
}

async function startTunnel() {
  if (state === 'public') return
  state = 'publishing'
  lastError = null
  try {
    hs = new Holesail({
      server: true,
      secure: true,
      host: '127.0.0.1',
      port: RELAY_PORT,
      key
    })
    await withTimeout(hs.ready(), READY_TIMEOUT_MS, 'tunnel did not become ready in time')
    state = 'public'
  } catch (err) {
    await closeInstance()
    state = 'error'
    lastError = err.message || String(err)
    throw err
  }
}

async function stopTunnel() {
  await closeInstance()
  state = 'private'
  lastError = null
}

async function regenerate() {
  const wasPublic = state === 'public'
  await closeInstance()
  try {
    fs.unlinkSync(KEY_FILE)
  } catch {}
  key = loadOrCreateKey(KEY_FILE)
  if (wasPublic) {
    await startTunnel()
  } else {
    await stopTunnel()
  }
}

function sendJSON(res, code, body) {
  res.writeHead(code, { 'Content-Type': 'application/json' })
  res.end(JSON.stringify(body))
}

// Queue a mutating op; reply 200 with status on success, 502 on failure.
function handleOp(res, fn) {
  op = op
    .then(fn)
    .then(() => sendJSON(res, 200, status()))
    .catch(() => sendJSON(res, 502, status()))
}

const server = http.createServer((req, res) => {
  const route = req.method + ' ' + req.url.split('?')[0]
  switch (route) {
    case 'GET /healthz':
      return sendJSON(res, 200, { ok: true })
    case 'GET /api/share/status':
      return sendJSON(res, 200, status())
    case 'POST /api/share/start':
      return handleOp(res, startTunnel)
    case 'POST /api/share/stop':
      return handleOp(res, stopTunnel)
    case 'POST /api/share/regenerate':
      return handleOp(res, regenerate)
    default:
      return sendJSON(res, 404, { error: 'not found' })
  }
})

server.on('error', (err) => {
  console.error(`control API listener failed on :${CONTROL_PORT}: ${err.message}`)
  process.exit(1)
})
server.listen(CONTROL_PORT, () => {
  console.log(`share control API on :${CONTROL_PORT} -> ${TARGET_HOST}:${TARGET_PORT} (${state})`)
})

async function shutdown() {
  relay.close()
  server.close()
  await closeInstance()
  process.exit(0)
}
process.on('SIGTERM', shutdown)
process.on('SIGINT', shutdown)
