import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    minify: false,
    rollupOptions: {
      output: {
        entryFileNames: 'assets/[name]-[hash].js',
        chunkFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash].[ext]'
      }
    }
  },
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    globals: true,
    pool: 'forks',
    maxWorkers: 1,
    testTimeout: 10000
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: false,
        ws: true
      },
      '/scripts': {
        target: 'http://localhost:8080',
        changeOrigin: false
      },
      '/downloads': {
        target: 'http://localhost:8080',
        changeOrigin: false
      }
    }
  }
})
