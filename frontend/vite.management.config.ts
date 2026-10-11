import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'node:path'

// Strictly local pilot. This Vite proxy only exposes the dedicated
// bearer-authenticated Management API, never the legacy Agent routes.
// Production requires a separately audited TLS ingress and Agent mTLS.
export default defineConfig({
  plugins: [vue()],
  server: {
    host: '127.0.0.1',
    port: 3002,
    strictPort: true,
    proxy: {
      '/api/v1/management': { target: 'http://127.0.0.1:8081', changeOrigin: false },
    },
  },
  preview: { host: '127.0.0.1', port: 3002, strictPort: true },
  build: { rollupOptions: { input: resolve(__dirname, 'management.html') } },
})
