export interface OpenFile {
  path: string
  name: string
  content: string
  lang: string
  savedContent?: string
  dirty?: boolean
}

export type OpenFilePatch = Partial<Pick<OpenFile, 'content' | 'lang' | 'savedContent' | 'dirty'>>

export function prepareOpenFile(file: OpenFile): OpenFile {
  const savedContent = file.savedContent ?? file.content
  return {
    ...file,
    savedContent,
    dirty: file.dirty ?? file.content !== savedContent,
  }
}

export function patchOpenFile(files: OpenFile[], index: number, patch: OpenFilePatch): OpenFile[] {
  if (index < 0 || index >= files.length) return files
  const current = files[index]
  const next = { ...current, ...patch }
  if (patch.content !== undefined && patch.dirty === undefined) {
    next.dirty = patch.content !== (next.savedContent ?? current.content)
  }
  const updated = [...files]
  updated[index] = next
  return updated
}

export function markOpenFileSaved(files: OpenFile[], path: string, content: string): OpenFile[] {
  let changed = false
  const updated = files.map(file => {
    if (file.path !== path) return file
    changed = true
    return { ...file, savedContent: content, dirty: file.content !== content }
  })
  return changed ? updated : files
}

export function dirtyPathBackedFiles(files: OpenFile[]): OpenFile[] {
  return files.filter(file => Boolean(file.path) && Boolean(file.dirty))
}

export function sameWorkspacePath(left: string, right: string): boolean {
  const normalise = (value: string) => value.trim().replaceAll('\\', '/').replace(/\/+$/, '').toLowerCase()
  const a = normalise(left)
  const b = normalise(right)
  return Boolean(a) && a === b
}
