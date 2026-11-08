import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from 'tailwindcss'
import autoprefixer from 'autoprefixer'
import { fileURLToPath } from 'node:url'

const path = (value: string) => fileURLToPath(new URL(value, import.meta.url))

// Deliberately separate root, assets, output and server: no production API proxy.
export default defineConfig({
  root: path('./prototype/'),
  publicDir: path('./public/'),
  cacheDir: path('./node_modules/.vite-prototype/'),
  plugins: [react()],
  css: { postcss: { plugins: [tailwindcss({
    content: [path('./prototype/**/*.{html,ts,tsx}'), path('./src/components/Toast.tsx')],
    theme: { extend: { colors: { success: 'var(--positive)', danger: 'var(--negative)' } } }
  }), autoprefixer()] } },
  build: { outDir: path('../bin/ui-prototype/'), emptyOutDir: true },
  server: { host: '127.0.0.1', port: 8081, strictPort: true },
  preview: { host: '127.0.0.1', port: 8081, strictPort: true },
  test: { environment: 'jsdom', setupFiles: path('./src/test/setup.ts'), globals: true, maxWorkers: 1 }
})
