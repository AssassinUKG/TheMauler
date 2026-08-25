import type { LabScopeTarget } from '../wailsjs/go'

export function splitScopeValues(value: string): string[] {
  return value.split(/[,;\r\n]+/).map(item => item.trim()).filter(Boolean)
}

export function scopeKind(raw: string): LabScopeTarget['kind'] {
  const value = raw.trim().replace(/^!\s*/, '')
  if (!value || /[\s,;]/.test(value)) return 'invalid'
  const slash = value.lastIndexOf('/')
  if (slash > 0) {
    const address = value.slice(0, slash).replace(/^\[|\]$/g, '')
    const prefix = Number(value.slice(slash + 1))
    if ((isIPv4(address) && prefix >= 0 && prefix <= 32) || (isIPv6(address) && prefix >= 0 && prefix <= 128)) return 'cidr'
  }
  if (isIPv4(value.replace(/^\[|\]$/g, '')) || isIPv6(value.replace(/^\[|\]$/g, ''))) return 'ip'
  try {
    const parsed = new URL(value)
    if ((parsed.protocol === 'http:' || parsed.protocol === 'https:') && parsed.hostname) return 'url'
  } catch {
    // Host, host:port, and host/path values are handled below.
  }
  try {
    const parsed = new URL(`http://${value}`)
    if (parsed.hostname && validHostname(parsed.hostname.replace(/^\[|\]$/g, ''))) {
      return isIPv4(parsed.hostname) || isIPv6(parsed.hostname.replace(/^\[|\]$/g, '')) ? 'ip' : 'hostname'
    }
  } catch {
    return 'invalid'
  }
  return 'invalid'
}

export function normaliseScopeTargets(entries: LabScopeTarget[] | undefined, legacyTarget = '', legacyHostname = ''): LabScopeTarget[] {
  const source: LabScopeTarget[] = entries?.length ? entries : [
    ...splitScopeValues(legacyTarget).map(value => blankScopeTarget(value)),
    ...(legacyHostname.trim() && legacyHostname.trim().toLowerCase() !== 'boxname.htb' ? [blankScopeTarget(legacyHostname)] : []),
  ]
  const out: LabScopeTarget[] = []
  const seen = new Set<string>()
  for (const entry of source) {
    for (let value of splitScopeValues(entry.value || '')) {
      let excluded = Boolean(entry.excluded)
      if (value.startsWith('!')) {
        excluded = true
        value = value.slice(1).trim()
      }
      if (!value || value.toLowerCase() === 'boxname.htb') continue
      const key = `${excluded}:${value.toLowerCase()}`
      if (seen.has(key)) continue
      seen.add(key)
      out.push({
        value,
        kind: scopeKind(value),
        environment: entry.environment === 'external' || entry.environment === 'internal' ? entry.environment : 'auto',
        label: (entry.label || '').trim().slice(0, 120),
        notes: (entry.notes || '').trim().slice(0, 500),
        excluded,
      })
    }
  }
  return out
}

export function blankScopeTarget(value = ''): LabScopeTarget {
  return { value, kind: scopeKind(value), environment: 'auto', label: '', notes: '', excluded: false }
}

export function primaryScopeTarget(entries: LabScopeTarget[]): string {
  return entries.find(entry => !entry.excluded && scopeKind(entry.value) !== 'invalid')?.value || ''
}

export function primaryScopeHostname(entries: LabScopeTarget[]): string {
  return entries.find(entry => !entry.excluded && scopeKind(entry.value) === 'hostname')?.value || ''
}

export function allowedScopeCount(entries: LabScopeTarget[] | undefined): number {
  return (entries || []).filter(entry => !entry.excluded && scopeKind(entry.value) !== 'invalid').length
}

export function replacePrimaryScopeTarget(entries: LabScopeTarget[] | undefined, value: string): LabScopeTarget[] {
  const next = normaliseScopeTargets(entries)
  const index = next.findIndex(entry => !entry.excluded)
  if (!value.trim()) {
    return index >= 0 ? next.filter((_, row) => row !== index) : next
  }
  if (index >= 0) {
    next[index] = { ...next[index], value: value.trim(), kind: scopeKind(value) }
    return next
  }
  return [blankScopeTarget(value.trim()), ...next]
}

function isIPv4(value: string): boolean {
  const parts = value.split('.')
  return parts.length === 4 && parts.every(part => /^\d{1,3}$/.test(part) && Number(part) >= 0 && Number(part) <= 255)
}

function isIPv6(value: string): boolean {
  if (!value.includes(':') || !/^[0-9a-f:]+$/i.test(value)) return false
  if ((value.match(/::/g) || []).length > 1) return false
  const parts = value.split(':')
  return parts.length >= 3 && parts.length <= 8 && parts.every(part => part === '' || /^[0-9a-f]{1,4}$/i.test(part))
}

function validHostname(value: string): boolean {
  if (!value || value.length > 253) return false
  return value.replace(/\.$/, '').split('.').every(label => Boolean(label) && label.length <= 63 && /^[a-z0-9_](?:[a-z0-9_-]*[a-z0-9_])?$/i.test(label))
}
