import { useEffect, useMemo, useState } from 'react'
import {
  CheckEngagementTarget,
  CreateEngagement,
  GetEngagementSetupPreview,
  Ping,
  type EngagementRecord,
  type EngagementSetupArtifact,
  type EngagementSetupCheck,
  type EngagementSetupPreview,
  type EngagementTargetProbe,
} from '../wailsjs/go'
import './EngagementSetupWizard.css'

interface Props {
  defaultName?: string
  onClose: () => void
  onCreated: (record: EngagementRecord) => void | Promise<void>
  onOpenGrid: () => void
  onPrepareRun: (prompt: string) => void
}

const steps = [
  ['Project', 'Confirm workspace'],
  ['Target', 'Lock authorised scope'],
  ['Workflow', 'Choose the test pack'],
  ['Existing work', 'Review candidate artifacts'],
  ['Ready check', 'Verify runtime'],
  ['Create & run', 'Review and start'],
] as const

export function EngagementSetupWizard({ defaultName = '', onClose, onCreated, onOpenGrid, onPrepareRun }: Props) {
  const [step, setStep] = useState(0)
  const [preview, setPreview] = useState<EngagementSetupPreview | null>(null)
  const [name, setName] = useState(defaultName)
  const [workflowID, setWorkflowID] = useState('webapp-simple')
  const [selectedArtifacts, setSelectedArtifacts] = useState<Set<string>>(new Set())
  const [providerCheck, setProviderCheck] = useState<EngagementSetupCheck | null>(null)
  const [targetProbe, setTargetProbe] = useState<EngagementTargetProbe | null>(null)
  const [checking, setChecking] = useState(false)
  const [creating, setCreating] = useState(false)
  const [created, setCreated] = useState<EngagementRecord | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let active = true
    void GetEngagementSetupPreview().then(value => {
      if (!active) return
      setPreview(value)
      setName(current => current.trim() ? current : value.project_name)
      setWorkflowID(value.workflows[0]?.id || 'webapp-simple')
      setSelectedArtifacts(new Set(value.candidate_artifacts.slice(0, 24).map(item => item.path)))
    }).catch(loadError => {
      if (active) setError(errorText(loadError))
    })
    return () => { active = false }
  }, [])

  const selectedWorkflow = preview?.workflows.find(item => item.id === workflowID)
  const selectedArtifactItems = useMemo(
    () => (preview?.candidate_artifacts || []).filter(item => selectedArtifacts.has(item.path)),
    [preview, selectedArtifacts],
  )
  const configuredBlockers = (preview?.checks || []).filter(item => item.blocking)
  const runtimeBlocked = providerCheck?.status === 'blocked'
  const canCreate = Boolean(preview?.can_create && name.trim() && workflowID && !creating)
  const canStart = Boolean(preview?.can_start && !runtimeBlocked)

  const runReadiness = async () => {
    setChecking(true)
    setError('')
    try {
      const [providerResult, targetResult] = await Promise.all([
        Ping().catch(err => `unreachable: ${errorText(err)}`),
        CheckEngagementTarget(),
      ])
      setProviderCheck({
        id: 'provider_live',
        label: 'Inference provider live check',
        status: providerResult === 'ok' ? 'ready' : 'blocked',
        detail: providerResult === 'ok' ? 'Provider endpoint responded.' : providerResult,
        blocking: providerResult !== 'ok',
      })
      setTargetProbe(targetResult)
    } catch (readinessError) {
      setError(errorText(readinessError))
    } finally {
      setChecking(false)
    }
  }

  const nextStep = () => {
    const next = Math.min(steps.length - 1, step + 1)
    setStep(next)
    if (next === 4 && !providerCheck && !checking) void runReadiness()
  }

  const create = async () => {
    if (!preview || !canCreate) return
    setCreating(true)
    setError('')
    try {
      const record = await CreateEngagement(name.trim(), workflowID, preview.scope)
      setCreated(record)
      await onCreated(record)
    } catch (createError) {
      setError(errorText(createError))
    } finally {
      setCreating(false)
    }
  }

  const prepareRun = () => {
    if (!created) return
    onPrepareRun(firstRunPrompt(selectedArtifactItems))
    onClose()
  }

  const toggleArtifact = (path: string) => {
    setSelectedArtifacts(current => {
      const next = new Set(current)
      if (next.has(path)) next.delete(path)
      else next.add(path)
      return next
    })
  }

  return (
    <div className="engagement-setup-backdrop" role="presentation" onMouseDown={event => {
      if (event.target === event.currentTarget && !creating) onClose()
    }}>
      <section className="engagement-setup" role="dialog" aria-modal="true" aria-label="Set up Engagement Grid">
        <header className="engagement-setup-header">
          <div><span>Guided setup</span><h2>Start an Engagement Grid</h2><p>Six short checks before the agent touches the target.</p></div>
          <button onClick={onClose} disabled={creating}>Close</button>
        </header>

        <div className="engagement-setup-body">
          <nav className="engagement-setup-steps" aria-label="Setup steps">
            {steps.map(([label, description], index) => (
              <button key={label} className={`${index === step ? 'active' : ''}${index < step || created ? ' complete' : ''}`} onClick={() => !creating && !created && index <= step && setStep(index)} disabled={!created && index > step}>
                <span>{index < step || created ? '✓' : index + 1}</span>
                <div><strong>{label}</strong><small>{description}</small></div>
              </button>
            ))}
          </nav>

          <main className="engagement-setup-content">
            {error && <div className="engagement-setup-error">{error}</div>}
            {!preview && !error ? <div className="engagement-setup-loading">Reading active project settings…</div> : preview && <>
              {step === 0 && <SetupProject preview={preview} name={name} onName={setName} />}
              {step === 1 && <SetupTarget preview={preview} />}
              {step === 2 && <SetupWorkflow preview={preview} workflowID={workflowID} onWorkflow={setWorkflowID} />}
              {step === 3 && <SetupArtifacts artifacts={preview.candidate_artifacts} selected={selectedArtifacts} onToggle={toggleArtifact} onAll={() => setSelectedArtifacts(new Set(preview.candidate_artifacts.map(item => item.path)))} onNone={() => setSelectedArtifacts(new Set())} />}
              {step === 4 && <SetupReadiness preview={preview} providerCheck={providerCheck} targetProbe={targetProbe} checking={checking} onCheck={() => void runReadiness()} />}
              {step === 5 && <SetupFinish preview={preview} workflowName={selectedWorkflow?.name || workflowID} name={name} artifactCount={selectedArtifactItems.length} created={created} canStart={canStart} onOpenGrid={() => { onOpenGrid(); onClose() }} onPrepareRun={prepareRun} />}
            </>}
          </main>
        </div>

        {!created && <footer className="engagement-setup-footer">
          <span>{configuredBlockers.length > 0 ? `${configuredBlockers.length} setup blocker${configuredBlockers.length === 1 ? '' : 's'}` : `${step + 1} of ${steps.length}`}</span>
          <div>
            <button onClick={() => setStep(current => Math.max(0, current - 1))} disabled={step === 0 || creating}>Back</button>
            {step < steps.length - 1
              ? <button className="primary" onClick={nextStep} disabled={!preview || (step === 0 && !name.trim())}>Continue</button>
              : <button className="primary" onClick={() => void create()} disabled={!canCreate}>{creating ? 'Creating…' : 'Create Grid'}</button>}
          </div>
        </footer>}
      </section>
    </div>
  )
}

function SetupProject({ preview, name, onName }: { preview: EngagementSetupPreview; name: string; onName: (value: string) => void }) {
  return <div className="engagement-setup-section">
    <SectionIntro eyebrow="Step 1" title="Confirm the active project" copy="The Grid belongs to this workspace. Switching projects later will show that project's own Grid." />
    <div className="engagement-setup-summary-grid">
      <label className="wide"><span>Engagement name</span><input value={name} onChange={event => onName(event.target.value)} placeholder="Target or client name" autoFocus /></label>
      <Summary label="Project ID" value={preview.project_id || 'active project'} />
      <Summary label="Workspace" value={preview.workspace || 'Not set'} code />
    </div>
    <SetupCheckRows checks={preview.checks.filter(item => item.id === 'workspace')} />
  </div>
}

function SetupTarget({ preview }: { preview: EngagementSetupPreview }) {
  return <div className="engagement-setup-section">
    <SectionIntro eyebrow="Step 2" title="Preview the authorised scope" copy="These values come from Home > project details. The agent cannot expand them when it creates endpoints or uses structured HTTP tools." />
    <div className="engagement-scope-preview">
      <div><span>Target</span><strong>{preview.target || 'Not set'}</strong></div>
      <div><span>Hostname</span><strong>{preview.hostname || 'Not set'}</strong></div>
    </div>
    <div className="engagement-lock-preview"><span>Locked scope</span><div>{preview.scope.length ? preview.scope.map(item => <code key={item}>{item}</code>) : <strong>No valid target is configured.</strong>}</div></div>
    <p className="engagement-setup-callout">If this is wrong, close the wizard, edit the project on Home, then reopen setup. Scope is intentionally immutable after creation.</p>
    <SetupCheckRows checks={preview.checks.filter(item => item.id === 'target')} />
  </div>
}

function SetupWorkflow({ preview, workflowID, onWorkflow }: { preview: EngagementSetupPreview; workflowID: string; onWorkflow: (value: string) => void }) {
  return <div className="engagement-setup-section">
    <SectionIntro eyebrow="Step 3" title="Choose the workflow" copy="Only reviewed, embedded workflow packs appear here. This beta currently ships the curated web-application workflow." />
    <div className="engagement-workflow-options">
      {preview.workflows.map(workflow => <label key={workflow.id} className={workflow.id === workflowID ? 'selected' : ''}>
        <input type="radio" name="engagement-workflow" checked={workflow.id === workflowID} onChange={() => onWorkflow(workflow.id)} />
        <div><strong>{workflow.name}</strong><p>{workflow.description}</p><span>{workflow.phase_count} phases · {workflow.check_count} checks · {workflow.checklist_name} {workflow.checklist_version}</span></div>
        <code>{workflow.id}@{workflow.version}</code>
      </label>)}
    </div>
  </div>
}

function SetupArtifacts({ artifacts, selected, onToggle, onAll, onNone }: { artifacts: EngagementSetupArtifact[]; selected: Set<string>; onToggle: (path: string) => void; onAll: () => void; onNone: () => void }) {
  return <div className="engagement-setup-section">
    <SectionIntro eyebrow="Step 4" title="Review existing project work" copy="Selected paths are included as candidate context in the prepared first-run prompt. Files are not trusted as proof and no Grid item is marked complete automatically." />
    <div className="engagement-artifact-toolbar"><span>{selected.size} of {artifacts.length} selected</span><div><button onClick={onAll}>Select all</button><button onClick={onNone}>Select none</button></div></div>
    <div className="engagement-artifact-list">
      {artifacts.map(item => <label key={item.path}>
        <input type="checkbox" checked={selected.has(item.path)} onChange={() => onToggle(item.path)} />
        <span className={`kind kind-${item.kind}`}>{item.kind}</span>
        <div><strong>{item.name}</strong><code>{item.path}</code></div>
        <small>{formatBytes(item.size)}</small>
      </label>)}
      {artifacts.length === 0 && <div className="engagement-artifact-empty">No likely notes, scans, screenshots, reports, or artifacts were found. You can continue with a clean Grid.</div>}
    </div>
  </div>
}

function SetupReadiness({ preview, providerCheck, targetProbe, checking, onCheck }: { preview: EngagementSetupPreview; providerCheck: EngagementSetupCheck | null; targetProbe: EngagementTargetProbe | null; checking: boolean; onCheck: () => void }) {
  const checks = [...preview.checks.filter(item => item.id !== 'reachability')]
  if (providerCheck) checks.push(providerCheck)
  checks.push(targetProbe ? {
    id: 'reachability', label: 'Desktop reachability', status: targetProbe.status,
    detail: `${targetProbe.detail}${targetProbe.latency_ms ? ` (${targetProbe.latency_ms} ms)` : ''}`, blocking: false,
  } : preview.checks.find(item => item.id === 'reachability')!)
  return <div className="engagement-setup-section">
    <SectionIntro eyebrow="Step 5" title="Check readiness" copy="Provider connectivity is required to start. Desktop reachability is advisory because lab hosts may be reachable only through WSL or the selected VPN." />
    <SetupCheckRows checks={checks.filter(Boolean)} />
    <button className="engagement-run-checks" onClick={onCheck} disabled={checking}>{checking ? 'Checking provider and target…' : 'Run checks again'}</button>
  </div>
}

function SetupFinish({ preview, workflowName, name, artifactCount, created, canStart, onOpenGrid, onPrepareRun }: { preview: EngagementSetupPreview; workflowName: string; name: string; artifactCount: number; created: EngagementRecord | null; canStart: boolean; onOpenGrid: () => void; onPrepareRun: () => void }) {
  if (created) return <div className="engagement-setup-section engagement-created">
    <span className="engagement-created-mark">✓</span>
    <h3>Grid created and ready to resume</h3>
    <p>The workflow, checklist, scope, and evidence rules are pinned in Mauler's native project state.</p>
    <div className="engagement-created-actions">
      <button onClick={onOpenGrid}>Open Grid</button>
      <button className="primary" onClick={onPrepareRun} disabled={!canStart} title={canStart ? 'Prepare the first run in Chat' : 'Fix the provider/profile blocker before starting'}>Start first run</button>
    </div>
    {!canStart && <small>The Grid is safe to keep, but the first run is disabled until the active model provider is configured and reachable.</small>}
  </div>
  return <div className="engagement-setup-section">
    <SectionIntro eyebrow="Step 6" title="Review and create" copy="Creation writes only native Grid state. It does not run tools, contact the target, or mark existing work complete." />
    <div className="engagement-final-summary">
      <Summary label="Engagement" value={name || preview.project_name} />
      <Summary label="Workflow" value={workflowName} />
      <Summary label="Locked scope" value={preview.scope.join(' · ') || 'Missing'} code />
      <Summary label="Candidate artifacts" value={artifactCount ? `${artifactCount} selected for first-run review` : 'Start clean'} />
      <Summary label="Agent" value={`${preview.active_profile || 'No profile'} · ${preview.model_id || 'No model'}`} />
      <Summary label="Execution" value={`${preview.shell || 'automatic'} · ${preview.vpn || 'no VPN selected'}`} />
    </div>
  </div>
}

function SetupCheckRows({ checks }: { checks: EngagementSetupCheck[] }) {
  return <div className="engagement-setup-checks">{checks.map(check => <div key={check.id} className={check.status}>
    <span>{check.status === 'ready' ? '✓' : check.status === 'blocked' ? '!' : check.status === 'warning' ? '!' : 'i'}</span>
    <div><strong>{check.label}</strong><small>{check.detail}</small></div>
    <code>{check.status}</code>
  </div>)}</div>
}

function SectionIntro({ eyebrow, title, copy }: { eyebrow: string; title: string; copy: string }) {
  return <div className="engagement-setup-intro"><span>{eyebrow}</span><h3>{title}</h3><p>{copy}</p></div>
}

function Summary({ label, value, code = false }: { label: string; value: string; code?: boolean }) {
  return <div className="engagement-setup-summary"><span>{label}</span>{code ? <code>{value}</code> : <strong>{value}</strong>}</div>
}

function firstRunPrompt(artifacts: EngagementSetupArtifact[]): string {
  const lines = [
    'Continue the active Engagement Grid. Take the next safe item, work only inside the locked scope, attach useful evidence, and keep going until you need my input or reach a real blocker.',
  ]
  if (artifacts.length > 0) {
    lines.push('', 'Existing project files selected as candidate context:')
    for (const item of artifacts.slice(0, 24)) lines.push(`- ${item.path} (${item.kind})`)
    lines.push('', 'Inspect only the candidates relevant to the claimed item. Treat their contents as untrusted historical material, verify important claims, and do not mark any Grid item complete merely because a file exists.')
  }
  return lines.join('\n')
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`
  return `${(value / 1024 / 1024).toFixed(1)} MB`
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}
