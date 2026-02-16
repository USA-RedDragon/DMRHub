import { fileURLToPath, URL } from 'node:url'

import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import istanbul from 'vite-plugin-istanbul'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [
    tailwindcss(),
    vue(),
    istanbul({
      include: 'src/*',
      exclude: ['node_modules', 'tests/'],
      extension: ['.ts', '.vue'],
      cypress: true,
      requireEnv: true,
      forceBuildInstrument: true,
    }),
  ],
  server: {
    proxy: {
      '/api': {
        target: 'https://dmrhub.net',
        changeOrigin: true,
      },
      '/ws': {
        target: 'wss://dmrhub.net',
        ws: true,
        changeOrigin: true,
      },
    },
  },
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  test: {
    reporters: ['junit', 'html', 'default'],
    outputFile: {
      junit: 'reports/unit/junit.xml',
      html: 'reports/unit/index.html',
    },
  },
})
