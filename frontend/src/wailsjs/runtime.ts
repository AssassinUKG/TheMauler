// Wails runtime shim — the real runtime is injected by the Wails host.
// During Vite dev mode this provides no-ops so the UI can run in a browser.

declare global {
  interface Window {
    runtime?: {
      EventsOn(event: string, cb: (...args: unknown[]) => void): () => void
      EventsOff(...events: string[]): void
      EventsEmit(event: string, ...data: unknown[]): void
      WindowMinimise(): void
      WindowMaximise(): void
      WindowClose(): void
    }
    go?: Record<string, Record<string, Record<string, (...args: unknown[]) => Promise<unknown>>>>
  }
}

function isWails(): boolean {
  return typeof window !== 'undefined' && !!window.runtime
}

export function EventsOn(
  event: string,
  cb: (...args: unknown[]) => void,
): () => void {
  if (isWails() && window.runtime) return window.runtime.EventsOn(event, cb)
  return () => {}
}

export function EventsOff(...events: string[]): void {
  if (isWails() && window.runtime) window.runtime.EventsOff(...events)
}
