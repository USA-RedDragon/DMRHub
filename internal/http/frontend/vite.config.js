import { fileURLToPath, URL } from 'node:url';

import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';
import istanbul from 'vite-plugin-istanbul';
import process from 'process';

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [
    vue(),
    istanbul({
      include: 'src/*',
      exclude: ['node_modules'],
      requireEnv: false,
      cypress: true,
      forceBuildInstrument: process.env.NODE_ENV === 'test',
    }),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  test: {
    reporter: ['junit', 'html', 'default'],
    outputFile: {
      junit: 'reports/unit/junit.xml',
      html: 'reports/unit/index.html',
    },
    coverage: {
      all: true,
      enabled: true,
      provider: 'c8',
      reporter: ['html', 'lcov', 'text'],
      reportsDirectory: 'coverage/unit',
      src: ['src'],
    },
  },
});
