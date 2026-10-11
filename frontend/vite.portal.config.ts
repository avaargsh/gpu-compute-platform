import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'node:path'

// Developer-only standalone entry; never expose the unauthenticated Go API
// through a public reverse proxy until control-plane RBAC is enforced.
export default defineConfig({
  plugins: [vue()],
  resolve: { alias: { '@': resolve(__dirname, 'src') } },
  server: {
    host: '127.0.0.1',
    port: 3001,
    strictPort: true,
    proxy: { '/api/v1': { target: 'http://127.0.0.1:8080', changeOrigin: false } },
  },
  preview: { host: '127.0.0.1', port: 3001, strictPort: true },
  build: {
    rollupOptions: { input: resolve(__dirname, 'portal.html') },
  },
})
