import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'static/dist',
    lib: {
      entry: 'static/scanner/ui/ScannerView.tsx',
      name: 'RCScanner',
      fileName: 'rc-scanner',
      formats: ['es']
    },
    rollupOptions: {
      // Keep react as a peer dep and preserve /static/* URL imports as-is
      external: ['react', 'react-dom', /^\/static\//]
    }
  }
})
