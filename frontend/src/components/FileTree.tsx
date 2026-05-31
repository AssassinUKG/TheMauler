import { useState, useEffect, useCallback, useRef } from 'react'
import {
  GetFileTree, GetHomeDir, GetWorkingDir, ReadFileContent,
  SelectWorkingDir, SetWorkingDir, RenameFile, DeleteFile, CreateFile, CreateDir,
  type FileNode,
} from '../wailsjs/go'
import './FileTree.css'

interface Props {
  onOpenFile?: (file: { path: string; name: string; content: string; lang: string }) => void
  onDropFile?: (path: string) => void
}

interface CtxMenu {
  x: number
  y: number
  node: FileNode
}

export function FileTree({ onOpenFile, onDropFile }: Props) {
  const [nodes, setNodes] = useState<FileNode[]>([])
  const [cwd, setCwd] = useState('')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [ctxMenu, setCtxMenu] = useState<CtxMenu | null>(null)
  const [renaming, setRenaming] = useState<string | null>(null)
  const [renameVal, setRenameVal] = useState('')
  const [creating, setCreating] = useState<{ dir: string; isDir: boolean } | null>(null)
  const [createVal, setCreateVal] = useState('')
  const menuRef = useRef<HTMLDivElement>(null)

  const refresh = useCallback(async (dir?: string) => {
    try {
      const d = dir ?? await GetWorkingDir()
      setCwd(d)
      const tree = await GetFileTree(d)
      setNodes(tree ?? [])
    } catch (_e) {}
  }, [])

  useEffect(() => { void refresh() }, [refresh])

  // Close context menu on outside click
  useEffect(() => {
    if (!ctxMenu) return
    const handler = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setCtxMenu(null)
      }
    }
    window.addEventListener('mousedown', handler)
    return () => window.removeEventListener('mousedown', handler)
  }, [ctxMenu])

  const toggleExpand = (path: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(path)) next.delete(path)
      else next.add(path)
      return next
    })
  }

  const changeDir = async (dir: string) => {
    if (!dir) return
    await SetWorkingDir(dir)
    setExpanded(new Set())
    await refresh()
  }

  const handleCwdChange = async () => {
    const dir = await SelectWorkingDir(cwd).catch(() => '')
    if (dir && dir !== cwd) await refresh(dir)
  }

  const goUp = async () => {
    const clean = cwd.replace(/[\\/]+$/, '')
    const parent = clean.replace(/[\\/][^\\/]*$/, '')
    if (parent && parent !== clean) await changeDir(parent)
  }

  const goHome = async () => {
    await changeDir(await GetHomeDir())
  }

  const openCtxMenu = (e: React.MouseEvent, node: FileNode) => {
    e.preventDefault()
    e.stopPropagation()
    setCtxMenu({ x: e.clientX, y: e.clientY, node })
  }

  const closeCtx = () => setCtxMenu(null)

  const ctxCopyPath = () => {
    if (!ctxMenu) return
    void navigator.clipboard.writeText(ctxMenu.node.path)
    closeCtx()
  }

  const ctxRename = () => {
    if (!ctxMenu) return
    setRenaming(ctxMenu.node.path)
    setRenameVal(ctxMenu.node.name)
    closeCtx()
  }

  const ctxDelete = async () => {
    if (!ctxMenu) return
    const node = ctxMenu.node
    closeCtx()
    if (!window.confirm(`Delete "${node.name}"?`)) return
    await DeleteFile(node.path).catch(() => {})
    await refresh()
  }

  const ctxNewFile = () => {
    if (!ctxMenu) return
    const dir = ctxMenu.node.isDir ? ctxMenu.node.path : ctxMenu.node.path.replace(/[\\/][^\\/]*$/, '')
    setCreating({ dir, isDir: false })
    setCreateVal('')
    closeCtx()
  }

  const ctxNewFolder = () => {
    if (!ctxMenu) return
    const dir = ctxMenu.node.isDir ? ctxMenu.node.path : ctxMenu.node.path.replace(/[\\/][^\\/]*$/, '')
    setCreating({ dir, isDir: true })
    setCreateVal('')
    closeCtx()
  }

  const submitRename = async () => {
    if (!renaming || !renameVal.trim()) { setRenaming(null); return }
    const dir = renaming.replace(/[\\/][^\\/]*$/, '')
    const newPath = dir + '/' + renameVal.trim()
    await RenameFile(renaming, newPath).catch(() => {})
    setRenaming(null)
    await refresh()
  }

  const submitCreate = async () => {
    if (!creating || !createVal.trim()) { setCreating(null); return }
    const newPath = creating.dir + '/' + createVal.trim()
    if (creating.isDir) {
      await CreateDir(newPath).catch(() => {})
    } else {
      await CreateFile(newPath).catch(() => {})
    }
    setCreating(null)
    await refresh()
  }

  return (
    <aside className="file-tree">
      <div className="file-tree-header">
        <span className="file-tree-title">EXPLORER</span>
        <button className="icon-btn" onClick={() => void goUp()} title="Parent directory">Up</button>
        <button className="icon-btn" onClick={() => void goHome()} title="Home directory">Home</button>
        <button className="icon-btn" onClick={() => void handleCwdChange()} title="Change directory">Cd</button>
        <button className="icon-btn" onClick={() => void refresh()} title="Refresh">Refresh</button>
      </div>
      <button className="file-tree-cwd" title={cwd} onClick={() => void handleCwdChange()}>
        <span className="file-tree-cwd-label">Workspace</span>
        <span className="file-tree-cwd-name">{workspaceName(cwd)}</span>
        <span className="file-tree-cwd-path">{cwd}</span>
      </button>

      {creating && (
        <div className="file-tree-create">
          <input
            autoFocus
            placeholder={creating.isDir ? 'folder name' : 'file name'}
            value={createVal}
            onChange={e => setCreateVal(e.target.value)}
            onKeyDown={e => {
              if (e.key === 'Enter') void submitCreate()
              if (e.key === 'Escape') setCreating(null)
            }}
            onBlur={() => void submitCreate()}
          />
        </div>
      )}

      <div className="file-tree-body">
        <NodeList
          nodes={nodes}
          expanded={expanded}
          onToggle={toggleExpand}
          onOpenFile={onOpenFile}
          onDropFile={onDropFile}
          onCtxMenu={openCtxMenu}
          renaming={renaming}
          renameVal={renameVal}
          onRenameChange={setRenameVal}
          onRenameSubmit={submitRename}
          onRenameCancel={() => setRenaming(null)}
          depth={0}
        />
      </div>

      {ctxMenu && (
        <div
          ref={menuRef}
          className="ctx-menu"
          style={{ top: ctxMenu.y, left: ctxMenu.x }}
        >
          <button onClick={ctxCopyPath}>Copy path</button>
          <button onClick={ctxRename}>Rename</button>
          <button onClick={ctxNewFile}>New file here</button>
          <button onClick={ctxNewFolder}>New folder here</button>
          <div className="ctx-divider" />
          <button className="ctx-danger" onClick={() => void ctxDelete()}>Delete</button>
        </div>
      )}
    </aside>
  )
}

function NodeList({
  nodes, expanded, onToggle, onOpenFile, onDropFile, onCtxMenu,
  renaming, renameVal, onRenameChange, onRenameSubmit, onRenameCancel, depth,
}: {
  nodes: FileNode[]
  expanded: Set<string>
  onToggle: (path: string) => void
  onOpenFile?: (file: { path: string; name: string; content: string; lang: string }) => void
  onDropFile?: (path: string) => void
  onCtxMenu: (e: React.MouseEvent, node: FileNode) => void
  renaming: string | null
  renameVal: string
  onRenameChange: (v: string) => void
  onRenameSubmit: () => void
  onRenameCancel: () => void
  depth: number
}) {
  return (
    <>
      {nodes.map(n => (
        <FileEntry
          key={n.path}
          node={n}
          expanded={expanded}
          onToggle={onToggle}
          onOpenFile={onOpenFile}
          onDropFile={onDropFile}
          onCtxMenu={onCtxMenu}
          renaming={renaming}
          renameVal={renameVal}
          onRenameChange={onRenameChange}
          onRenameSubmit={onRenameSubmit}
          onRenameCancel={onRenameCancel}
          depth={depth}
        />
      ))}
    </>
  )
}

function FileEntry({
  node, expanded, onToggle, onOpenFile, onDropFile, onCtxMenu,
  renaming, renameVal, onRenameChange, onRenameSubmit, onRenameCancel, depth,
}: {
  node: FileNode
  expanded: Set<string>
  onToggle: (path: string) => void
  onOpenFile?: (file: { path: string; name: string; content: string; lang: string }) => void
  onDropFile?: (path: string) => void
  onCtxMenu: (e: React.MouseEvent, node: FileNode) => void
  renaming: string | null
  renameVal: string
  onRenameChange: (v: string) => void
  onRenameSubmit: () => void
  onRenameCancel: () => void
  depth: number
}) {
  const isOpen = expanded.has(node.path)
  const indent = depth * 12

  const openFile = async () => {
    if (node.isDir) return
    const content = await ReadFileContent(node.path)
    const ext = node.name.includes('.') ? node.name.split('.').pop() ?? 'plaintext' : 'plaintext'
    onOpenFile?.({ path: node.path, name: node.name, content, lang: languageFromExt(ext) })
  }

  const handleDragStart = (e: React.DragEvent) => {
    e.dataTransfer.setData('text/plain', node.path)
    e.dataTransfer.effectAllowed = 'copy'
  }

  return (
    <div>
      {renaming === node.path ? (
        <div className="file-entry" style={{ paddingLeft: `${8 + indent}px` }}>
          <input
            autoFocus
            className="file-rename-input"
            value={renameVal}
            onChange={e => onRenameChange(e.target.value)}
            onKeyDown={e => {
              if (e.key === 'Enter') onRenameSubmit()
              if (e.key === 'Escape') onRenameCancel()
            }}
            onBlur={onRenameSubmit}
          />
        </div>
      ) : (
        <div
          className={`file-entry ${node.isDir ? 'dir' : 'file'}`}
          style={{ paddingLeft: `${8 + indent}px` }}
          onClick={() => node.isDir ? onToggle(node.path) : void openFile()}
          onDoubleClick={() => { void openFile() }}
          onContextMenu={e => onCtxMenu(e, node)}
          draggable={!node.isDir}
          onDragStart={handleDragStart}
          title={node.path}
        >
          <span className="file-icon">{fileIcon(node)}</span>
          <span className="file-name">{node.name}</span>
        </div>
      )}
      {node.isDir && isOpen && node.children && (
        <NodeList
          nodes={node.children}
          expanded={expanded}
          onToggle={onToggle}
          onOpenFile={onOpenFile}
          onDropFile={onDropFile}
          onCtxMenu={onCtxMenu}
          renaming={renaming}
          renameVal={renameVal}
          onRenameChange={onRenameChange}
          onRenameSubmit={onRenameSubmit}
          onRenameCancel={onRenameCancel}
          depth={depth + 1}
        />
      )}
    </div>
  )
}

function fileIcon(node: FileNode): string {
  if (node.isDir) return '📁'
  const ext = node.name.includes('.') ? node.name.split('.').pop()!.toLowerCase() : ''
  const icons: Record<string, string> = {
    ts: '🔷', tsx: '🔷', js: '🟨', jsx: '🟨',
    go: '🟦', py: '🐍', rs: '🦀', rb: '💎',
    md: '📝', txt: '📄', json: '{}', yaml: '📋', yml: '📋', toml: '📋',
    css: '🎨', scss: '🎨', html: '🌐', xml: '🌐', svg: '🖼️',
    sh: '⚡', ps1: '⚡', bat: '⚡',
    png: '🖼️', jpg: '🖼️', jpeg: '🖼️', gif: '🖼️', webp: '🖼️',
    pdf: '📕', zip: '📦', tar: '📦', gz: '📦',
    env: '🔒', lock: '🔒',
  }
  return icons[ext] ?? '📄'
}

function workspaceName(path: string): string {
  const clean = path.replace(/[\\/]+$/, '')
  const parts = clean.split(/[\\/]/).filter(Boolean)
  return parts[parts.length - 1] || clean || 'Select folder'
}

function languageFromExt(ext: string): string {
  const map: Record<string, string> = {
    css: 'css', go: 'go', html: 'html',
    js: 'javascript', json: 'json', jsx: 'javascript',
    md: 'markdown', ps1: 'powershell', py: 'python',
    sh: 'shell', ts: 'typescript', tsx: 'typescript',
    xml: 'xml', yaml: 'yaml', yml: 'yaml',
  }
  return map[ext.toLowerCase()] ?? ext.toLowerCase()
}
