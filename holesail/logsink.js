// Structured log sink for the logd aggregator. Writes flat JSON lines — the
// exact on-disk shape logd parses ({time,level,msg,service}) — to
// <LOG_DIR>/<service>.log on the shared volume, and tees human text to the
// original console so `docker logs` still works. LOG_DIR unset (local/dev) or a
// file-open failure ⇒ console only, never a crash.
//
// Node parity note: this captures JS-level crashes (uncaughtException /
// unhandledRejection) but NOT a native segfault or a raw fd-2 write — Node
// can't alias fd 2 from JS the way the Go producers' dup2 does. Acceptable for
// this small sidecar; documented gap.

const fs = require('fs')
const path = require('path')
const util = require('util')

const CAP = 20 * 1024 * 1024 // rotate past ~20 MB (matches the Go producers)
const GENERATIONS = 5

// formatArgs renders console-style varargs into one message string (objects via
// util.inspect); JSON.stringify later escapes any newlines to keep one line.
function formatArgs(args) {
  return args.map((a) => (typeof a === 'string' ? a : util.inspect(a))).join(' ')
}

// makeSink opens the service log file and returns a writer, or null if it can't
// open (caller then stays console-only). No global state touched — unit-testable.
function makeSink(dir, service) {
  const file = path.join(dir, service + '.log')
  let fd = null
  let size = 0
  function open() {
    fd = fs.openSync(file, 'a') // O_APPEND|O_CREATE; throws synchronously on failure
    try { size = fs.fstatSync(fd).size } catch { size = 0 }
  }
  try {
    open()
  } catch {
    return null // caller stays console-only
  }

  function rotate() {
    try {
      fs.closeSync(fd)
      for (let i = GENERATIONS - 1; i >= 1; i--) {
        const from = `${file}.${i}`
        if (fs.existsSync(from)) fs.renameSync(from, `${file}.${i + 1}`)
      }
      fs.renameSync(file, `${file}.1`)
    } catch { /* fall through and reopen */ }
    try { open() } catch { fd = null }
  }

  return {
    file,
    write(level, msg) {
      const line = JSON.stringify({ time: new Date().toISOString(), level, msg, service }) + '\n'
      try {
        if (size + line.length > CAP) rotate()
        if (fd != null) {
          fs.writeSync(fd, line)
          size += line.length
        }
      } catch { /* never let logging crash the service */ }
    },
  }
}

// initLogSink wires the global console + crash handlers to the file sink. Call
// once, early, before other logging.
function initLogSink(service) {
  const dir = process.env.LOG_DIR
  if (!dir) return
  const sink = makeSink(dir, service)
  const origLog = console.log.bind(console)
  const origErr = console.error.bind(console)
  if (!sink) {
    origErr(`log sink disabled: cannot open ${path.join(dir, service + '.log')}`)
    return
  }
  console.log = (...a) => { sink.write('INFO', formatArgs(a)); origLog(...a) }
  console.error = (...a) => { sink.write('ERROR', formatArgs(a)); origErr(...a) }
  process.on('uncaughtException', (err) => {
    sink.write('ERROR', 'uncaughtException: ' + ((err && err.stack) || String(err)))
    origErr(err)
    process.exit(1)
  })
  process.on('unhandledRejection', (reason) => {
    sink.write('ERROR', 'unhandledRejection: ' + ((reason && reason.stack) || String(reason)))
    origErr(reason)
  })
}

module.exports = { initLogSink, makeSink, formatArgs }
