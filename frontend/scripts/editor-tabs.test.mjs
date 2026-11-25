import assert from 'node:assert/strict'
import test from 'node:test'
import {
  dirtyPathBackedFiles,
  markOpenFileSaved,
  patchOpenFile,
  prepareOpenFile,
  sameWorkspacePath,
} from '../src/editorTabs.ts'

test('each editor tab retains its own draft and saved baseline', () => {
  const files = [
    prepareOpenFile({ path: 'C:/work/a.txt', name: 'a.txt', content: 'saved a', lang: 'plaintext' }),
    prepareOpenFile({ path: 'C:/work/b.txt', name: 'b.txt', content: 'saved b', lang: 'plaintext' }),
  ]
  const edited = patchOpenFile(files, 0, { content: 'draft a' })

  assert.equal(edited[0].content, 'draft a')
  assert.equal(edited[0].savedContent, 'saved a')
  assert.equal(edited[0].dirty, true)
  assert.deepEqual(edited[1], files[1])
})

test('saving advances the baseline and clears dirty state', () => {
  const edited = patchOpenFile([
    prepareOpenFile({ path: 'C:/work/a.txt', name: 'a.txt', content: 'saved', lang: 'plaintext' }),
  ], 0, { content: 'draft' })
  const saved = markOpenFileSaved(edited, 'C:/work/a.txt', 'draft')

  assert.equal(saved[0].content, 'draft')
  assert.equal(saved[0].savedContent, 'draft')
  assert.equal(saved[0].dirty, false)
})

test('a save completion never overwrites text typed while the write was pending', () => {
  const files = [
    prepareOpenFile({ path: 'C:/work/a.txt', name: 'a.txt', content: 'first draft', lang: 'plaintext', savedContent: 'saved', dirty: true }),
  ]
  const typedAgain = patchOpenFile(files, 0, { content: 'newer draft' })
  const completed = markOpenFileSaved(typedAgain, 'C:/work/a.txt', 'first draft')

  assert.equal(completed[0].content, 'newer draft')
  assert.equal(completed[0].savedContent, 'first draft')
  assert.equal(completed[0].dirty, true)
})

test('workspace transitions guard only dirty path-backed tabs', () => {
  const files = [
    prepareOpenFile({ path: 'C:/work/a.txt', name: 'a.txt', content: 'saved', lang: 'plaintext', savedContent: 'old', dirty: true }),
    prepareOpenFile({ path: '', name: 'snippet.md', content: 'scratch draft', lang: 'markdown', savedContent: '', dirty: true }),
    prepareOpenFile({ path: 'C:/work/clean.txt', name: 'clean.txt', content: 'saved', lang: 'plaintext' }),
  ]

  assert.deepEqual(dirtyPathBackedFiles(files).map(file => file.name), ['a.txt'])
})

test('workspace comparison is Windows separator and case insensitive', () => {
  assert.equal(sameWorkspacePath('C:\\Work\\Repo\\', 'c:/work/repo'), true)
  assert.equal(sameWorkspacePath('C:/work/one', 'C:/work/two'), false)
  assert.equal(sameWorkspacePath('', ''), false)
})
