import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rolldownOptions: {
      output: {
        strictExecutionOrder: true,
        codeSplitting: {
          groups: [
            {
              name: 'monaco',
              test: /node_modules[\\/](?:@monaco-editor|monaco-editor|dompurify|marked|state-local)[\\/]/,
              minSize: 0,
              maxSize: 2_000_000,
              priority: 20,
            },
            {
              name: 'vendor',
              test: /node_modules[\\/]/,
              minSize: 100_000,
              maxSize: 400_000,
              priority: 10,
            },
          ],
        },
      },
    },
  },
  server: {
    port: 5173,
    strictPort: true,
  },
})
