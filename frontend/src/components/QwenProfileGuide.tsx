import type { Profile } from '../wailsjs/go'
import './QwenProfileGuide.css'

interface Props {
  profile: Profile
  active: boolean
  reasoningEffort: string
  benchmarking: boolean
  onReasoningEffortChange: (effort: string) => void
  onApplyOfficial: () => void
  onBenchmark: () => void
}

const closeTo = (actual: number, expected: number) => Math.abs(actual - expected) < 0.0001

export function isQwen38Profile(profile: Profile | undefined): boolean {
  if (!profile) return false
  return /qwen[\s._-]*3[\s._-]*8/i.test(`${profile.name} ${profile.model_id}`)
}

export function QwenProfileGuide({
  profile,
  active,
  reasoningEffort,
  benchmarking,
  onReasoningEffortChange,
  onApplyOfficial,
  onBenchmark,
}: Props) {
  if (!isQwen38Profile(profile)) return null

  const think = profile.thinking_general
  const direct = profile.nothinking
  const thinkingSamplerReady = profile.thinking &&
    closeTo(think.temperature, 1) && closeTo(think.top_p, 0.95) && think.top_k === 20 &&
    closeTo(think.min_p, 0) && closeTo(think.presence_penalty, 0) && closeTo(think.repeat_penalty, 1)
  const directSamplerReady = closeTo(direct.temperature, 0.7) && closeTo(direct.top_p, 0.8) &&
    direct.top_k === 20 && closeTo(direct.min_p, 0) && closeTo(direct.presence_penalty, 1.5) &&
    closeTo(direct.repeat_penalty, 1)
  const localContextReady = profile.ctx_tokens === 35000
  const kvReady = (profile.kv_cache_precision || 'f16') === 'f16' &&
    (profile.kv_cache_type_k || 'f16') === 'f16' && (profile.kv_cache_type_v || 'f16') === 'f16'
  const mtpReady = profile.spec_type === 'draft-mtp' && profile.spec_draft_n_max === 2

  return (
    <section className="qwen-profile-guide" aria-label="Qwen 3.8 local setup">
      <div className="qwen-guide-head">
        <div>
          <div className="qwen-guide-kicker">Qwen 3.8 · local agent setup</div>
          <h4>Recommended starting point for this RTX 3090</h4>
          <p>
            Qwen supports a much larger native window. Mauler keeps this local profile at the measured
            35K working budget so the 27B Q4 model, FP16 cache, tools, and output reserve fit safely in 24 GB.
          </p>
        </div>
        <span className={`qwen-active-badge ${active ? 'active' : ''}`}>
          {active ? 'Current default' : 'Saved candidate'}
        </span>
      </div>

      <div className="qwen-guide-grid">
        <GuideCheck ready={thinkingSamplerReady} label="Thinking sampler" value="1.0 · 0.95 · top-k 20" />
        <GuideCheck ready={directSamplerReady} label="Direct sampler" value="0.7 · 0.8 · presence 1.5" />
        <GuideCheck ready={profile.preserve_thinking} label="Reasoning continuity" value={profile.preserve_thinking ? 'Preserved between turns' : 'Not preserved'} />
        <GuideCheck ready={localContextReady} label="Local context" value={`${profile.ctx_tokens.toLocaleString()} tokens`} />
        <GuideCheck ready={kvReady} label="KV cache" value={`${(profile.kv_cache_precision || 'f16').toUpperCase()} quality mode`} />
        <GuideCheck ready={mtpReady} label="MTP draft" value={mtpReady ? 'n=2 · benchmark required' : 'Off or custom'} review />
      </div>

      <div className="qwen-reasoning-control">
        <div>
          <strong>Starting reasoning depth</strong>
          <span>Auto is recommended: Mauler uses medium normally and xhigh for planning and final review.</span>
        </div>
        <select value={reasoningEffort || 'auto'} onChange={event => onReasoningEffortChange(event.target.value)}>
          <option value="auto">Auto · recommended</option>
          <option value="none">Direct · no new thinking</option>
          <option value="low">Low · quick reasoning</option>
          <option value="medium">Medium · balanced</option>
          <option value="xhigh">XHigh · deepest</option>
        </select>
      </div>

      <div className="qwen-guide-actions">
        <button className="primary" type="button" onClick={onApplyOfficial}>Apply official settings</button>
        <button type="button" onClick={onBenchmark} disabled={benchmarking}>
          {benchmarking ? 'Benchmarking...' : 'Benchmark this profile'}
        </button>
        <span>MTP is provisional. Unattended status still requires a fresh 12/12 gate followed by pass^5.</span>
      </div>
    </section>
  )
}

function GuideCheck({ ready, label, value, review = false }: { ready: boolean; label: string; value: string; review?: boolean }) {
  const state = ready ? (review ? 'review' : 'ready') : 'attention'
  return (
    <div className={`qwen-guide-check ${state}`}>
      <span className="qwen-check-dot" aria-hidden="true" />
      <div>
        <strong>{label}</strong>
        <span>{value}</span>
      </div>
    </div>
  )
}
