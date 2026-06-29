#!/usr/bin/env node
// Guards against drift between the two TypeScript views of the Go data model:
//   - src/wailsjs/go.ts        (hand-maintained; this is what the app compiles against)
//   - ../wailsjs/go/models.ts  (Wails-generated bindings)
//
// They are structured differently (inline types vs generated classes), so we can't
// diff them line-for-line. But every Go struct field carries a snake_case json tag,
// so the SET of snake_case field names must be identical in both files. A mismatch
// means one file was updated and the other wasn't — exactly the trap where the
// generated file gained a field the build-critical hand-written file lacked.
//
// Exits non-zero (failing CI / the build) when the field sets diverge.

import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

const here = dirname(fileURLToPath(import.meta.url))
const HANDWRITTEN = resolve(here, '../src/wailsjs/go.ts')
const GENERATED = resolve(here, '../wailsjs/go/models.ts')

// Match identifiers used as object keys that contain at least one underscore,
// i.e. Go json-tag style snake_case fields. camelCase function/method names and
// single-word keys are intentionally ignored.
const FIELD_RE = /\b([a-z][a-z0-9]*(?:_[a-z0-9]+)+)\s*:/g

function snakeFields(path) {
  const text = readFileSync(path, 'utf8')
  const set = new Set()
  for (const m of text.matchAll(FIELD_RE)) set.add(m[1])
  return set
}

function diff(a, b) {
  return [...a].filter((x) => !b.has(x)).sort()
}

const hand = snakeFields(HANDWRITTEN)
const gen = snakeFields(GENERATED)

const onlyHand = diff(hand, gen)
const onlyGen = diff(gen, hand)

if (onlyHand.length === 0 && onlyGen.length === 0) {
  console.log(`✓ type model in sync (${hand.size} snake_case fields match)`)
  process.exit(0)
}

console.error('✗ TypeScript model drift detected between:')
console.error(`    src/wailsjs/go.ts        (hand-maintained, gates the build)`)
console.error(`    wailsjs/go/models.ts     (Wails-generated)`)
if (onlyHand.length) console.error(`\n  Only in src/wailsjs/go.ts:        ${onlyHand.join(', ')}`)
if (onlyGen.length) console.error(`\n  Only in wailsjs/go/models.ts:     ${onlyGen.join(', ')}`)
console.error('\nAdd the missing field(s) to whichever file lacks them so both mirror the Go structs.')
process.exit(1)
