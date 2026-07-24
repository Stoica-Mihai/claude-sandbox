const { test } = require('node:test')
const assert = require('node:assert')
const fs = require('fs')
const os = require('os')
const path = require('path')
const { makeSink, formatArgs } = require('./logsink')

function tmpdir() {
  return fs.mkdtempSync(path.join(os.tmpdir(), 'holesail-log-'))
}

test('writes a flat JSON line in the shape logd parses', () => {
  const dir = tmpdir()
  const sink = makeSink(dir, 'holesail')
  sink.write('INFO', 'share control API up')
  const lines = fs.readFileSync(path.join(dir, 'holesail.log'), 'utf8').trim().split('\n')
  assert.equal(lines.length, 1)
  const rec = JSON.parse(lines[0])
  assert.equal(rec.service, 'holesail')
  assert.equal(rec.level, 'INFO')
  assert.equal(rec.msg, 'share control API up')
  assert.ok(!Number.isNaN(Date.parse(rec.time)), 'time is a valid timestamp')
})

test('error level is preserved', () => {
  const dir = tmpdir()
  const sink = makeSink(dir, 'holesail')
  sink.write('ERROR', 'relay listener failed')
  const rec = JSON.parse(fs.readFileSync(path.join(dir, 'holesail.log'), 'utf8').trim())
  assert.equal(rec.level, 'ERROR')
})

test('formatArgs renders objects and joins varargs', () => {
  assert.equal(formatArgs(['a', 'b']), 'a b')
  assert.match(formatArgs(['state', { n: 1 }]), /state .*n: 1/)
})

test('multi-line message stays one JSON line', () => {
  const dir = tmpdir()
  const sink = makeSink(dir, 'holesail')
  sink.write('ERROR', 'line1\nline2')
  const raw = fs.readFileSync(path.join(dir, 'holesail.log'), 'utf8')
  assert.equal(raw.trim().split('\n').length, 1, 'embedded newline must be escaped, not split')
  assert.equal(JSON.parse(raw.trim()).msg, 'line1\nline2')
})

test('makeSink returns null when the dir cannot be opened', () => {
  // A path whose parent is a file → open fails → caller stays console-only.
  const dir = tmpdir()
  const asFile = path.join(dir, 'notadir')
  fs.writeFileSync(asFile, 'x')
  assert.equal(makeSink(path.join(asFile, 'sub'), 'holesail'), null)
})
