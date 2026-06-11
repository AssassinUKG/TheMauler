import { useEffect, useRef, useState } from 'react'
import Editor, { DiffEditor } from '@monaco-editor/react'
import type { OnMount } from '@monaco-editor/react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { SaveFileContent, RunArtifact, StopArtifact } from '../wailsjs/go'
import './FileViewer.css'

export interface OpenFile {
  path: string
  name: string
  content: string
  lang: string
}

interface Props {
  file: OpenFile | null
  artifactOutput?: string
  artifactRunning?: boolean
  onArtifactOutputClear?: () => void
}

const RUNNABLE = new Set(['bash', 'sh', 'shell', 'python', 'python3', 'py', 'javascript', 'js', 'node', 'typescript', 'ts', 'powershell', 'pwsh', 'ps1'])

export function FileViewer({ file, artifactOutput = '', artifactRunning = false, onArtifactOutputClear }: Props) {
  const [content, setContent] = useState('')
  const [language, setLanguage] = useState('plaintext')
  const [dirty, setDirty] = useState(false)
  const [status, setStatus] = useState('')
  const [position, setPosition] = useState('Ln 1, Col 1')
  const [showOutput, setShowOutput] = useState(false)
  const [showDiff, setShowDiff] = useState(false)
  const [showPreview, setShowPreview] = useState(false)
  const editorRef = useRef<Parameters<OnMount>[0] | null>(null)
  const outputRef = useRef<HTMLPreElement>(null)

  useEffect(() => {
    setContent(file?.content ?? '')
    setLanguage(file?.lang ?? 'plaintext')
    setDirty(false)
    setStatus('')
    setPosition('Ln 1, Col 1')
    setShowDiff(false)
    setShowPreview(false)
  }, [file?.path, file?.content, file?.lang])

  // Leaving markdown drops out of preview mode
  useEffect(() => {
    if (language !== 'markdown') setShowPreview(false)
  }, [language])

  // Re-layout Monaco after exiting preview (it was hidden, not unmounted)
  useEffect(() => {
    if (!showPreview) {
      window.setTimeout(() => editorRef.current?.layout(), 50)
    }
  }, [showPreview])

  useEffect(() => {
    if (artifactOutput) {
      setShowOutput(true)
      if (outputRef.current) {
        outputRef.current.scrollTop = outputRef.current.scrollHeight
      }
    }
  }, [artifactOutput])

  const save = async () => {
    if (!file || !file.path) return
    await SaveFileContent(file.path, content)
    setDirty(false)
    setStatus('Saved')
    window.setTimeout(() => setStatus(''), 1600)
  }

  const format = async () => {
    await editorRef.current?.getAction('editor.action.formatDocument')?.run()
  }

  const runCode = async () => {
    if (!file) return
    onArtifactOutputClear?.()
    setShowOutput(true)
    await RunArtifact(language, content).catch(e => console.error('RunArtifact:', e))
  }

  const stopCode = () => {
    void StopArtifact()
  }

  const handleMount: OnMount = (editor, monaco) => {
    editorRef.current = editor
    editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => {
      void save()
    })
    editor.onDidChangeCursorPosition(e => {
      setPosition(`Ln ${e.position.lineNumber}, Col ${e.position.column}`)
    })
    window.setTimeout(() => editor.layout(), 0)
  }

  const canRun = file != null && RUNNABLE.has(language)

  if (!file) {
    return (
      <div className="file-viewer-empty">
        <div className="file-viewer-empty-icon">FILE</div>
        <div>Select a file in Explorer to view it here</div>
        <div className="file-viewer-empty-hint">Or click <strong>Open</strong> on a code block in chat</div>
      </div>
    )
  }

  return (
    <div className="file-viewer">
      <div className="file-viewer-header">
        <div className="file-viewer-title">
          <span>{file.name}</span>
          {dirty && <span className="file-viewer-dirty">modified</span>}
          {status && <span className="file-viewer-status">{status}</span>}
        </div>
        <div className="file-viewer-actions">
          <select value={language} onChange={e => setLanguage(e.target.value)} title="Syntax language">
            {languages.map(lang => <option key={lang} value={lang}>{lang}</option>)}
          </select>
          {language === 'markdown' && (
            <div className="fv-seg" role="group" aria-label="Markdown view">
              <button
                className={!showPreview ? 'active-btn' : ''}
                onClick={() => setShowPreview(false)}
                title="Edit source"
              >Edit</button>
              <button
                className={showPreview ? 'active-btn' : ''}
                onClick={() => { setShowPreview(true); setShowDiff(false) }}
                title="Rendered preview"
              >Preview</button>
            </div>
          )}
          {!showDiff && !showPreview && <button onClick={() => void format()}>Format</button>}
          {dirty && !showPreview && (
            <button
              className={showDiff ? 'active-btn' : ''}
              onClick={() => setShowDiff(v => !v)}
              title="Toggle diff vs saved"
            >
              {showDiff ? 'Edit' : 'Diff'}
            </button>
          )}
          {canRun && (
            artifactRunning
              ? <button className="danger" onClick={stopCode}>Stop</button>
              : <button className="run-btn" onClick={() => void runCode()}>Run</button>
          )}
          {artifactOutput && (
            <button onClick={() => setShowOutput(v => !v)} title="Toggle output">
              {showOutput ? 'Hide output' : 'Show output'}
            </button>
          )}
          <button className="primary" onClick={() => void save()} disabled={!dirty || !file.path}>Save</button>
        </div>
      </div>

      <div className="file-viewer-path" title={file.path}>{file.path || 'scratch snippet'}</div>

      <div className={`file-viewer-body ${showOutput && artifactOutput ? 'has-output' : ''}`}>
        <div className="file-viewer-editor">
          {/* Monaco stays mounted at all times — hidden behind the preview overlay when showPreview */}
          <div className="file-viewer-editor-inner" style={showPreview ? { visibility: 'hidden', pointerEvents: 'none' } : undefined}>
            {showDiff ? (
              <DiffEditor
                height="100%"
                language={language}
                original={file?.content ?? ''}
                modified={content}
                theme="vs-dark"
                options={{
                  fontSize: 13,
                  fontFamily: 'Cascadia Code, Fira Code, Consolas, monospace',
                  renderSideBySide: true,
                  automaticLayout: true,
                  scrollBeyondLastLine: false,
                  wordWrap: 'on',
                  readOnly: false,
                  originalEditable: false,
                }}
              />
            ) : (
              <Editor
                height="100%"
                language={language}
                value={content}
                theme="vs-dark"
                onMount={handleMount}
                onChange={value => {
                  setContent(value ?? '')
                  setDirty(true)
                  setStatus('')
                }}
                options={{
                  bracketPairColorization: { enabled: true },
                  cursorBlinking: 'smooth',
                  cursorSmoothCaretAnimation: 'on',
                  folding: true,
                  fontLigatures: true,
                  fontSize: 13,
                  fontFamily: 'Cascadia Code, Fira Code, Consolas, monospace',
                  lineNumbers: 'on',
                  matchBrackets: 'always',
                  minimap: { enabled: true, side: 'right', size: 'proportional', showSlider: 'mouseover' },
                  overviewRulerBorder: false,
                  renderLineHighlight: 'all',
                  renderWhitespace: 'selection',
                  roundedSelection: false,
                  rulers: [100],
                  smoothScrolling: true,
                  scrollBeyondLastLine: false,
                  automaticLayout: true,
                  wordWrap: 'on',
                  tabSize: 2,
                  trimAutoWhitespace: true,
                }}
              />
            )}
          </div>
          {showPreview && (
            <div className="md-preview file-viewer-preview-overlay">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
            </div>
          )}
        </div>

        {showOutput && artifactOutput && (
          <div className="artifact-output-panel">
            <div className="artifact-output-header">
              <span>Output</span>
              {artifactRunning && <span className="artifact-running-dot" />}
              <button onClick={() => { onArtifactOutputClear?.(); setShowOutput(false) }}>Clear</button>
            </div>
            <pre ref={outputRef} className="artifact-output-body">{artifactOutput}</pre>
          </div>
        )}
      </div>

      <div className="file-viewer-statusbar">
        <span>{language}</span>
        <span>{position}</span>
        <span>{content.length.toLocaleString()} chars</span>
      </div>
    </div>
  )
}

const languages = [
  'plaintext',
  'go',
  'typescript',
  'javascript',
  'json',
  'markdown',
  'css',
  'html',
  'python',
  'powershell',
  'shell',
  'yaml',
  'xml',
]
