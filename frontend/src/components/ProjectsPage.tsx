import { useEffect, useMemo, useState } from 'react'
import {
  AddWorkspaceFolder,
  CreateDir,
  GetEngagementNext,
  GetSettings,
  GetWorkingDir,
  ListEngagements,
  ListVPNInterfaces,
  ScaffoldWorkspaceFolders,
  SelectWorkspaceFolder,
  UpdateSettings,
  type EngagementNextAction,
  type EngagementRecord,
  type EngagementSummary,
  type LabProfile,
  type Settings,
  type VPNInterfaceInfo,
  type WorkspaceFolder,
} from '../wailsjs/go'
import { EngagementSetupWizard } from './EngagementSetupWizard'
import './ProjectsPage.css'

interface Props {
  version: number
  onProjectChanged: (project: LabProfile, previousName: string) => void
  onOpenEngagement: () => void
  onPrepareEngagementRun: (prompt: string) => void
}

const defaultFolders = ['notes', 'scans', 'loot', 'scripts', 'screenshots']

export function ProjectsPage({ version, onProjectChanged, onOpenEngagement, onPrepareEngagementRun }: Props) {
  const [settings, setSettings] = useState<Settings | null>(null)
  const [cwd, setCwd] = useState('')
  const [selectedId, setSelectedId] = useState('')
  const [draft, setDraft] = useState<LabProfile>(blankProject())
  const [vpnItems, setVPNItems] = useState<VPNInterfaceInfo[]>([])
  const [folderDraft, setFolderDraft] = useState(defaultFolders.join(' '))
  const [status, setStatus] = useState('')
  const [editing, setEditing] = useState(false)
  const [engagement, setEngagement] = useState<EngagementSummary | null>(null)
  const [engagementNext, setEngagementNext] = useState<EngagementNextAction | null>(null)
  const [setupOpen, setSetupOpen] = useState(false)

  const load = async () => {
    const [nextSettings, nextCwd, interfaces, engagementItems] = await Promise.all([
      GetSettings(),
      GetWorkingDir().catch(() => ''),
      ListVPNInterfaces().catch(() => [] as VPNInterfaceInfo[]),
      ListEngagements().catch(() => [] as EngagementSummary[]),
    ])
    const profiles = normaliseProfiles(nextSettings)
    const active = nextSettings.context.active_lab_profile || nextSettings.context.lab.id || profiles[0]?.id || ''
    setSettings({ ...nextSettings, context: { ...nextSettings.context, lab_profiles: profiles } })
    setCwd(nextCwd)
    setVPNItems(interfaces)
    setSelectedId(active)
    setDraft(profileToDraft(profiles.find(item => item.id === active) ?? profiles[0] ?? blankProject(), nextSettings.context.workspace_dir || nextCwd))
    const currentEngagement = engagementItems[0] ?? null
    setEngagement(currentEngagement)
    setEngagementNext(currentEngagement ? await GetEngagementNext(currentEngagement.id).catch(() => null) : null)
  }

  useEffect(() => {
    void load()
  }, [version])

  const projects = settings?.context.lab_profiles ?? []
  const activeId = settings?.context.active_lab_profile || settings?.context.lab.id || ''
  const selected = useMemo(
    () => projects.find(item => item.id === selectedId) ?? null,
    [projects, selectedId],
  )

  useEffect(() => {
    if (selected) setDraft(profileToDraft(selected, settings?.context.workspace_dir || cwd))
  }, [selected?.id])

  const showStatus = (message: string) => {
    setStatus(message)
    window.setTimeout(() => setStatus(current => current === message ? '' : current), 2400)
  }

  const chooseProject = (id: string) => {
    const project = projects.find(item => item.id === id)
    if (!project) return
    setSelectedId(id)
    setDraft(profileToDraft(project, settings?.context.workspace_dir || cwd))
    setEditing(false)
  }

  const newProject = () => {
    const base = settings?.context.workspace_dir || cwd || 'C:/Users/richa/Documents/HTB_writeups'
    const name = 'New HTB box'
    const id = uniqueProjectId('new-box', projects)
    setSelectedId('')
    setDraft({
      ...blankProject(),
      id,
      name,
      workspace_dir: joinPath(base, id),
      ops_profile: 'HTB / CTF',
      evidence_policy: 'discovery_first',
      access_preference: 'auto',
    })
    setEditing(true)
  }

  const pickRoot = async () => {
    const picked = await SelectWorkspaceFolder(draft.workspace_dir || settings?.context.workspace_dir || cwd).catch(() => '')
    if (picked) setDraft(prev => ({ ...prev, workspace_dir: picked }))
  }

  const saveProject = async (activate = false) => {
    if (!settings) return null
    const profile = cleanProfile(draft, settings.context.workspace_dir || cwd, projects)
    const profiles = upsertProject(projects, profile)
    const next: Settings = {
      ...settings,
      context: {
        ...settings.context,
        lab_profiles: profiles,
        active_lab_profile: activate ? profile.id : settings.context.active_lab_profile,
        lab: activate ? labFromProfile(profile) : settings.context.lab,
        workspace_dir: activate ? profile.workspace_dir : settings.context.workspace_dir,
        open_folders: activate ? workspaceFoldersFor(profile.workspace_dir, foldersFromDraft(folderDraft)) : settings.context.open_folders,
      },
    }
    const previousName = projects.find(item => item.id === activeId)?.name || settings.context.lab.name || 'previous workspace'
    if (activate) {
      await ensureProjectFolders(profile.workspace_dir, foldersFromDraft(folderDraft))
    }
    await UpdateSettings(next)
    setSettings(next)
    setSelectedId(profile.id)
    setDraft(profile)
    setEditing(false)
    showStatus(activate ? `Switched to ${profile.name}` : `Saved ${profile.name}`)
    if (activate) onProjectChanged(profile, previousName)
    return profile
  }

  const deleteProject = async (id: string) => {
    if (!settings) return
    const target = projects.find(item => item.id === id)
    if (!target) return
    if (!confirm(`Delete project "${target.name || target.id}" from the list? Files on disk are not deleted.`)) return
    const profiles = projects.filter(item => item.id !== id)
    const nextActive = settings.context.active_lab_profile === id ? (profiles[0]?.id || '') : settings.context.active_lab_profile
    const nextActiveProfile = profiles.find(item => item.id === nextActive)
    const next: Settings = {
      ...settings,
      context: {
        ...settings.context,
        lab_profiles: profiles,
        active_lab_profile: nextActive,
        lab: nextActiveProfile ? labFromProfile(nextActiveProfile) : settings.context.lab,
        workspace_dir: nextActiveProfile?.workspace_dir || settings.context.workspace_dir,
        open_folders: nextActiveProfile ? workspaceFoldersFor(nextActiveProfile.workspace_dir, defaultFolders) : settings.context.open_folders,
      },
    }
    await UpdateSettings(next)
    setSettings(next)
    setSelectedId(nextActive)
    setDraft(profileToDraft(nextActiveProfile ?? blankProject(), settings.context.workspace_dir || cwd))
    showStatus('Project removed')
    if (settings.context.active_lab_profile === id && nextActiveProfile) onProjectChanged(nextActiveProfile, target.name || target.id)
  }

  const selectedVPN = vpnItems.find(item => vpnValue(item) === draft.vpn_interface || item.name === draft.vpn_interface)

  const startEngagement = async () => {
    if (selectedId !== activeId) {
      showStatus('Open this box before creating its engagement grid')
      return
    }
    setSetupOpen(true)
  }

  const acceptCreatedEngagement = async (created: EngagementRecord) => {
    const summary: EngagementSummary = {
      id: created.state.id,
      name: created.state.name,
      workspace: created.workspace,
      workflow_id: created.workflow.id,
      workflow_version: created.workflow.version,
      checklist_id: created.checklist.id,
      checklist_version: created.checklist.version,
      current_phase: created.state.current_phase,
      revision: created.state.revision,
      created_at: created.state.created_at,
      updated_at: created.state.updated_at,
    }
    setEngagement(summary)
    setEngagementNext(await GetEngagementNext(summary.id).catch(() => null))
    showStatus('Engagement grid created')
  }

  return (
    <div className="projects-page">
      <header className="projects-header">
        <div>
          <span className="project-kicker">Start here</span>
          <h1>Your boxes</h1>
          <p>Resume an old box or create a clean workspace. Files and saved sessions are never deleted when you switch.</p>
        </div>
        <div className="projects-header-actions">
          {status && <span className="projects-status">{status}</span>}
          <button onClick={() => void load()}>Refresh</button>
          <button className="project-new-box" onClick={newProject}>+ New HTB box</button>
        </div>
      </header>

      <div className="projects-layout">
        <aside className="projects-list">
          {projects.length === 0 ? (
            <div className="project-empty">No saved projects yet.</div>
          ) : projects.map(project => (
            <button
              key={project.id}
              className={`project-card${project.id === selectedId ? ' selected' : ''}${project.id === activeId ? ' active' : ''}`}
              onClick={() => chooseProject(project.id)}
            >
              <span className="project-card-title">{project.name || project.id}{project.id === activeId ? '  • ACTIVE' : ''}</span>
              <span>{[project.target, project.hostname, project.vpn_interface].filter(Boolean).join(' | ') || 'No target set'}</span>
              <span className="project-card-root">{project.workspace_dir || cwd}</span>
            </button>
          ))}
        </aside>

        <section className={`project-editor${editing ? ' is-editing' : ''}`}>
          <div className="project-editor-head project-resume-head">
            <div>
              <div className="project-kicker">{editing ? 'Edit project details' : selectedId === activeId ? 'Active project' : 'Selected project'}</div>
              <h2>{draft.name || 'Untitled project'}</h2>
              {!editing && <p>{selectedId === activeId ? 'Ready to continue with the current workspace and project context.' : 'Open this box to switch workspace and start a clean project chat.'}</p>}
            </div>
            <div className="project-editor-actions">
              {editing ? (
                <>
                  {selectedId && <button onClick={() => { setDraft(profileToDraft(selected ?? blankProject(), settings?.context.workspace_dir || cwd)); setEditing(false) }}>Cancel</button>}
                  <button onClick={() => void saveProject(false)}>Save details</button>
                  <button className="primary" onClick={() => void saveProject(true)}>{selectedId === activeId ? 'Save & resume' : 'Save & open'}</button>
                </>
              ) : (
                <>
                  <button onClick={() => setEditing(true)}>Edit details</button>
                  <button className="primary project-resume-button" onClick={() => void saveProject(true)}>{selectedId === activeId ? 'Resume box' : 'Open box'}</button>
                </>
              )}
            </div>
          </div>

          {!editing ? (
            <div className="project-resume-dashboard">
              <div className="project-resume-cards">
                <div>
                  <span>Target</span>
                  <strong>{draft.target || 'Not set'}</strong>
                  <code>{draft.hostname || 'No hostname'}</code>
                </div>
                <div>
                  <span>Environment</span>
                  <strong>{selectedVPN?.name || draft.vpn_interface || 'Automatic route'}</strong>
                  <small>{selectedVPN?.ip ? `${selectedVPN.ip}/${selectedVPN.cidr}` : 'VPN/interface not pinned'}</small>
                </div>
                <div>
                  <span>Latest evidence</span>
                  <strong>{draft.latest_artifact || 'No artifact yet'}</strong>
                  <small>{draft.evidence_policy ? draft.evidence_policy.replaceAll('_', ' ') : 'Default evidence policy'}</small>
                </div>
              </div>

              <div className="project-continue-panel">
                <div className="project-continue-head">
                  <div>
                    <span className="project-kicker">Continue where you left off</span>
                    <h3>{draft.latest_artifact ? 'Evidence is ready for the next step' : 'Start with project discovery'}</h3>
                  </div>
                  <button onClick={() => setEditing(true)}>Review project context</button>
                </div>
                <div className="project-continue-row">
                  <span className="project-state-dot active" />
                  <div><strong>Workspace</strong><small>{draft.workspace_dir || cwd || 'Not set'}</small></div>
                </div>
                <div className="project-continue-row">
                  <span className="project-state-dot" />
                  <div><strong>Working profile</strong><small>{draft.ops_profile || 'Pentesting'} · {draft.access_preference || 'automatic access'}</small></div>
                </div>
                {selectedId === activeId && (engagement ? (
                  <div className="project-engagement-card">
                    <span className="project-state-dot active" />
                    <div>
                      <span className="project-kicker">Engagement grid</span>
                      <strong>{engagement.name}</strong>
                      <small>{engagement.current_phase.replaceAll('_', ' ')} · {engagementNext?.work?.title || (engagementNext?.phase_complete ? 'phase ready to advance' : 'ready to resume')}</small>
                    </div>
                    <button onClick={onOpenEngagement}>Open Grid</button>
                  </div>
                ) : (
                  <div className="project-engagement-card empty">
                    <span className="project-state-dot" />
                    <div>
                      <span className="project-kicker">Engagement grid</span>
                      <strong>Start governed testing</strong>
                      <small>{draft.target || draft.hostname ? 'Pins the reviewed workflow, checklist, and locked project scope.' : 'Set a target or hostname before creating a grid.'}</small>
                    </div>
                    <button onClick={() => void startEngagement()} disabled={!draft.target && !draft.hostname}>Create Grid</button>
                  </div>
                ))}
                {draft.notes && <div className="project-notes-preview"><span>Project note</span><p>{draft.notes}</p></div>}
              </div>
            </div>
          ) : <>
          <div className="project-form-grid">
            <label>
              <span>Name</span>
              <input value={draft.name} onChange={e => setDraft(prev => ({ ...prev, name: e.target.value }))} placeholder="Box name / client project" />
            </label>
            <label>
              <span>Project id</span>
              <input value={draft.id} onChange={e => setDraft(prev => ({ ...prev, id: slug(e.target.value) }))} placeholder="connected" />
            </label>
            <label className="wide">
              <span>Agent root folder</span>
              <div className="project-root-row">
                <input value={draft.workspace_dir} onChange={e => setDraft(prev => ({ ...prev, workspace_dir: e.target.value }))} placeholder="C:/Users/richa/Documents/HTB_writeups/boxname" />
                <button onClick={() => void pickRoot()}>Browse</button>
              </div>
            </label>
            <label>
              <span>Target IP / URL</span>
              <input value={draft.target} onChange={e => setDraft(prev => ({ ...prev, target: e.target.value }))} placeholder="10.129.x.x or https://host" />
            </label>
            <label>
              <span>Hostname</span>
              <input value={draft.hostname} onChange={e => setDraft(prev => ({ ...prev, hostname: e.target.value }))} placeholder="boxname.htb" />
            </label>
            <label>
              <span>VPN/interface</span>
              <select value={draft.vpn_interface} onChange={e => setDraft(prev => ({ ...prev, vpn_interface: e.target.value }))}>
                <option value="">auto / not set</option>
                {vpnItems.map(item => <option key={`${item.name}-${item.ip}`} value={vpnValue(item)}>{item.label}</option>)}
              </select>
            </label>
            <label>
              <span>Access preference</span>
              <select value={draft.access_preference || 'auto'} onChange={e => setDraft(prev => ({ ...prev, access_preference: e.target.value }))}>
                <option value="auto">auto</option>
                <option value="webshell">webshell</option>
                <option value="reverse_shell">reverse shell</option>
                <option value="bind_shell">bind shell</option>
                <option value="none">none</option>
              </select>
            </label>
            <label>
              <span>Ops profile</span>
              <select value={draft.ops_profile || 'Pentesting'} onChange={e => setDraft(prev => ({ ...prev, ops_profile: e.target.value }))}>
                <option value="Pentesting">Pentesting</option>
                <option value="HTB / CTF">HTB / CTF</option>
              </select>
            </label>
            <label>
              <span>Evidence policy</span>
              <select value={draft.evidence_policy || defaultEvidencePolicy(draft.ops_profile)} onChange={e => setDraft(prev => ({ ...prev, evidence_policy: e.target.value }))}>
                <option value="discovery_first">Discovery-first</option>
                <option value="research_assisted">Research-assisted</option>
                <option value="reference_allowed">Reference allowed</option>
                <option value="fastest_path">Fastest path</option>
              </select>
            </label>
            <label>
              <span>Latest artifact</span>
              <input value={draft.latest_artifact} onChange={e => setDraft(prev => ({ ...prev, latest_artifact: e.target.value }))} placeholder="scans/nmap_full.xml" />
            </label>
            <label className="wide">
              <span>Folder scaffold</span>
              <input value={folderDraft} onChange={e => setFolderDraft(e.target.value)} placeholder="notes scans loot scripts screenshots" />
            </label>
            <label className="wide">
              <span>Project notes</span>
              <textarea value={draft.notes} onChange={e => setDraft(prev => ({ ...prev, notes: e.target.value }))} placeholder="Per-box constraints, known bad paths, client scope, do-not-retry notes..." />
            </label>
          </div>

          <div className="project-summary">
            <div><span>Active root</span><strong>{settings?.context.workspace_dir || cwd || 'not set'}</strong></div>
            <div><span>Selected VPN</span><strong>{selectedVPN ? `${selectedVPN.ip}/${selectedVPN.cidr} (${selectedVPN.name})` : draft.vpn_interface || 'not set'}</strong></div>
            <div><span>Open / resume</span><strong>switches root, starts a clean project chat, and preserves previous files and sessions</strong></div>
          </div>
          {selectedId && <div className="project-danger-zone"><div><strong>Remove project from Mauler</strong><span>Files on disk are never deleted.</span></div><button className="danger" onClick={() => void deleteProject(selectedId)}>Delete project</button></div>}
          </>}
        </section>
      </div>
      {setupOpen && <EngagementSetupWizard
        defaultName={draft.name}
        onClose={() => setSetupOpen(false)}
        onCreated={acceptCreatedEngagement}
        onOpenGrid={onOpenEngagement}
        onPrepareRun={onPrepareEngagementRun}
      />}
    </div>
  )
}

function blankProject(): LabProfile {
  return {
    id: '',
    name: '',
    workspace_dir: '',
    target: '',
    hostname: '',
    vpn_interface: '',
    latest_artifact: '',
    ops_profile: 'Pentesting',
    evidence_policy: 'research_assisted',
    access_preference: 'auto',
    notes: '',
  }
}

function normaliseProfiles(settings: Settings): LabProfile[] {
  const profiles = [...(settings.context.lab_profiles ?? [])]
  if (profiles.length === 0) {
    profiles.push({
      ...blankProject(),
      ...settings.context.lab,
      id: settings.context.lab.id || 'default',
      name: settings.context.lab.name || 'HTB / Pentest box',
      workspace_dir: settings.context.workspace_dir,
    })
  }
  return profiles
}

function profileToDraft(profile: LabProfile, fallbackRoot: string): LabProfile {
  return {
    ...blankProject(),
    ...profile,
    id: profile.id || slug(profile.name || 'project'),
    name: profile.name || profile.id || 'Project',
    workspace_dir: profile.workspace_dir || fallbackRoot,
    ops_profile: profile.ops_profile || 'Pentesting',
    evidence_policy: profile.evidence_policy || defaultEvidencePolicy(profile.ops_profile),
    access_preference: profile.access_preference || 'auto',
  }
}

function cleanProfile(draft: LabProfile, fallbackRoot: string, existing: LabProfile[]): LabProfile {
  const wantedId = slug(draft.id || draft.name || 'project')
  const current = existing.find(item => item.id === draft.id)
  const id = current || existing.every(item => item.id !== wantedId) ? wantedId : uniqueProjectId(wantedId, existing)
  return {
    ...blankProject(),
    ...draft,
    id,
    name: draft.name.trim() || id,
    workspace_dir: slashPath((draft.workspace_dir || joinPath(fallbackRoot, id)).trim()),
    ops_profile: draft.ops_profile || 'Pentesting',
    evidence_policy: draft.evidence_policy || defaultEvidencePolicy(draft.ops_profile),
    access_preference: draft.access_preference || 'auto',
  }
}

function upsertProject(projects: LabProfile[], profile: LabProfile): LabProfile[] {
  const next = projects.filter(item => item.id !== profile.id)
  return [profile, ...next]
}

function labFromProfile(profile: LabProfile) {
  return {
    id: profile.id,
    name: profile.name,
    target: profile.target,
    hostname: profile.hostname,
    vpn_interface: profile.vpn_interface,
    latest_artifact: profile.latest_artifact,
    ops_profile: profile.ops_profile,
    evidence_policy: profile.evidence_policy || defaultEvidencePolicy(profile.ops_profile),
    access_preference: profile.access_preference,
    notes: profile.notes,
  }
}

function defaultEvidencePolicy(opsProfile: string): string {
  return /htb|ctf/i.test(opsProfile || '') ? 'discovery_first' : 'research_assisted'
}

function workspaceFoldersFor(root: string, folders: string[]): WorkspaceFolder[] {
  return [
    { path: slashPath(root), name: basename(root), role: 'root' },
    ...folders.map(name => ({ path: joinPath(root, name), name, role: folderRole(name) })),
  ]
}

async function ensureProjectFolders(root: string, folders: string[]) {
  await CreateDir(root)
  await ScaffoldWorkspaceFolders(root, folders).catch(() => [])
  await Promise.all(folders.map(name => AddWorkspaceFolder(joinPath(root, name), folderRole(name)).catch(() => [])))
}

function foldersFromDraft(value: string): string[] {
  const items = value.split(/[,\s]+/).map(item => item.trim()).filter(Boolean)
  return items.length > 0 ? Array.from(new Set(items)) : defaultFolders
}

function folderRole(name: string): string {
  const n = name.toLowerCase()
  if (['notes', 'scans', 'loot', 'scripts', 'screenshots'].includes(n)) return n
  return 'folder'
}

function slug(value: string): string {
  return value.trim().toLowerCase().replace(/[^a-z0-9_.-]+/g, '-').replace(/^-+|-+$/g, '') || 'project'
}

function uniqueProjectId(base: string, projects: LabProfile[]): string {
  const root = slug(base)
  let id = root
  let i = 2
  while (projects.some(item => item.id === id)) {
    id = `${root}-${i}`
    i += 1
  }
  return id
}

function joinPath(root: string, name: string): string {
  const cleanRoot = slashPath(root || '').replace(/\/+$/, '')
  return `${cleanRoot}/${name}`.replace(/^\/+/, '')
}

function slashPath(path: string): string {
  return path.replace(/\\/g, '/')
}

function basename(path: string): string {
  const clean = slashPath(path).replace(/\/+$/, '')
  return clean.split('/').pop() || 'Project'
}

function vpnValue(item: VPNInterfaceInfo): string {
  return item.ip ? `${item.name}:${item.ip}` : item.name
}
