export function replyOutcomeNotice(status?: string, reason?: string): { label: string; detail: string } | null {
  if (reason === 'output_limit') return { label: 'Response limit reached', detail: 'This answer is partial. Continue from here or send a new request.' }
  if (status === 'done' || status === 'recovered' || !status) return null
  if (reason === 'user_stopped') return { label: 'Stopped by you', detail: 'The partial response was kept. No further generation is running.' }
  if (status === 'error') return { label: 'Response interrupted', detail: 'The run failed before a verified final handoff. Check the run details before retrying actions.' }
  return { label: 'Run stopped', detail: 'This is a retained partial response, not a confirmed completion.' }
}
