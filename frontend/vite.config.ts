import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { resolve } from 'path'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    react({
      babel: {
        plugins: [
          ['babel-plugin-react-compiler', { target: '19' }],
        ],
      },
    }),
  ],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
    },
  },
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: 'http://localhost:8000',
        changeOrigin: true,
      },
      '/ws': {
        target: 'ws://localhost:8000',
        ws: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: true,
    rollupOptions: {
      input: {
        main: resolve(__dirname, 'index.html'),
        portal: resolve(__dirname, 'portal.html'),
        management: resolve(__dirname, 'management.html'),
      },
      output: {
        manualChunks: (id) => {
          if (id.includes('echarts')) return 'echarts'
          if (id.includes('react-router')) return 'router'
          if (id.includes('@radix-ui')) return 'radix-ui'
          if (id.includes('lucide-react') || id.includes('clsx') || id.includes('tailwind-merge') || id.includes('class-variance-authority')) return 'ui'
          if (id.includes('@tanstack/react-query')) return 'query'
          if (id.includes('react') || id.includes('react-dom')) return 'vendor'
        },
      },
    },
  },
})
