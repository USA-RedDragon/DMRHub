import { fileURLToPath, URL } from 'node:url';

import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [
    vue(),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    proxy: {
      '/api/v1/features': {
        bypass: (req, res) => {
          res.setHeader('Content-Type', 'application/json');
          res.end(JSON.stringify({ features: [''] }));
        },
      },
      '/api/v1/network/name': {
        bypass: (req, res) => {
          res.setHeader('Content-Type', 'text/plain');
          res.end('DMRHub');
        },
      },
      '/api/v1/me': {
        bypass: (req, res) => {
          res.setHeader('Content-Type', 'application/json');
          res.end(JSON.stringify({}));
        },
      },
      '/api/v1/version': {
        bypass: (req, res) => {
          res.setHeader('Content-Type', 'text/plain');
          res.end('Test');
        },
      },
    },
  },
  test: {
    reporter: ['junit', 'html', 'default'],
    outputFile: {
      junit: 'reports/unit/junit.xml',
      html: 'reports/unit/index.html',
    },
  },
});
