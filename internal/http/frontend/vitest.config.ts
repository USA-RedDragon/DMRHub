import { fileURLToPath } from 'node:url'
import { mergeConfig, defineConfig, configDefaults } from 'vitest/config'
import viteConfig from './vite.config'

export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      environment: 'jsdom',
      exclude: [...configDefaults.exclude, 'e2e/**'],
      root: fileURLToPath(new URL('./', import.meta.url)),
      coverage: {
        provider: 'v8',
        reportsDirectory: 'coverage/unit',
        reporter: ['lcov', 'cobertura', 'text-summary'],
        include: ['src/**/*.{ts,vue}'],
        exclude: [
          '**/*.d.ts',
          '**/components/ui/**',
          '**/main.ts',
        ],
        excludeAfterRemap: true,
      },
    },
  }),
)
