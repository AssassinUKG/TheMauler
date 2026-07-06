import { Component, StrictMode, type ErrorInfo, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'

class RootErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('TheMauler UI crashed', error, info)
  }

  render() {
    if (!this.state.error) return this.props.children
    return (
      <main style={{
        minHeight: '100vh',
        padding: 24,
        background: '#161616',
        color: '#e5e5e5',
        fontFamily: 'system-ui, sans-serif',
      }}>
        <h1 style={{ fontSize: 18, margin: '0 0 12px' }}>TheMauler UI hit a render error</h1>
        <p style={{ color: '#a8a8a8', maxWidth: 760 }}>
          The backend is probably still running. Restart the app, or copy this error for debugging.
        </p>
        <pre style={{
          marginTop: 16,
          padding: 16,
          maxWidth: 980,
          whiteSpace: 'pre-wrap',
          overflow: 'auto',
          background: '#0d1117',
          border: '1px solid #30363d',
          borderRadius: 8,
        }}>{this.state.error.stack || this.state.error.message}</pre>
      </main>
    )
  }
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <RootErrorBoundary>
      <App />
    </RootErrorBoundary>
  </StrictMode>,
)
