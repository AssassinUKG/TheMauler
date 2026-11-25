import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

const distDir = new URL('../dist/', import.meta.url)
const assetsDir = new URL('./assets/', distDir)
const html = readFileSync(new URL('./index.html', distDir), 'utf8')
const initialAssets = new Set(
  [...html.matchAll(/(?:src|href)="\/assets\/([^"?]+\.js)"/g)].map(match => match[1]),
)

const initialSource = [...initialAssets]
  .map(name => readFileSync(new URL(`./assets/${name}`, distDir), 'utf8'))
  .join('\n')

if (/MonacoEnvironment|createDiffEditor|vs\/editor\/editor\.main/.test(initialSource)) {
  throw new Error('Monaco editor runtime is still present in the initial application path')
}

const javascriptAssets = readdirSync(assetsDir).filter(name => name.endsWith('.js'))
const lazySource = javascriptAssets
  .filter(name => !initialAssets.has(name))
  .map(name => readFileSync(new URL(`./assets/${name}`, distDir), 'utf8'))
  .join('\n')

if (!/MonacoEnvironment|createDiffEditor/.test(lazySource)) {
  throw new Error('The lazy editor path does not contain the local Monaco runtime')
}

const entry = [...html.matchAll(/<script[^>]+src="\/assets\/([^"?]+\.js)"/g)][0]?.[1]
if (!entry) throw new Error('Production entry script was not found')
const initialSizes = [...initialAssets].map(name => ({
  name,
  bytes: statSync(join(fileURLToPath(assetsDir), name)).size,
}))
const initialGraphBytes = initialSizes.reduce((total, asset) => total + asset.bytes, 0)
const largestInitialAsset = initialSizes.sort((left, right) => right.bytes - left.bytes)[0]

if (!largestInitialAsset) throw new Error('Production initial JavaScript graph was empty')
if (largestInitialAsset.bytes >= 500_000) {
  throw new Error(
    `Largest initial JavaScript asset ${largestInitialAsset.name} is ${largestInitialAsset.bytes} bytes; expected less than 500000`,
  )
}
if (initialGraphBytes >= 1_250_000) {
  throw new Error(`Initial JavaScript graph is ${initialGraphBytes} bytes; expected less than 1250000`)
}

console.log(
  `✓ Monaco is lazy/local; initial graph ${initialGraphBytes} bytes across ${initialSizes.length} assets; largest ${largestInitialAsset.name} ${largestInitialAsset.bytes} bytes`,
)
