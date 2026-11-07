/* eslint-disable no-undef */
import { defineConfig } from 'cypress';
import process from 'process';
import cypressMochawesomeReporter from 'cypress-mochawesome-reporter/plugin.js';
import codeCoverage from '@cypress/code-coverage/task';

/**
 * softwareWebGL lets Chrome draw WebGL2 with SwiftShader on a host with no
 * GPU, such as a CI runner, where Chrome otherwise refuses WebGL and the map
 * cannot load.
 */
export function softwareWebGL(browser: Cypress.Browser, launchOptions: Cypress.BeforeBrowserLaunchOptions) {
  if (browser.family === 'chromium' && browser.name !== 'electron') {
    launchOptions.args.push('--enable-unsafe-swiftshader');
  }
  return launchOptions;
}

export default defineConfig({
  video: process.env.BROWSER !== 'firefox',
  reporter: 'cypress-multi-reporters',
  reporterOptions: {
    reporterEnabled: 'cypress-mochawesome-reporter, mocha-junit-reporter',
    mochaJunitReporterReporterOptions: {
      mochaFile: 'reports/e2e/junit.xml',
    },
    cypressMochawesomeReporterReporterOptions: {
      charts: true,
      embeddedScreenshots: true,
      inlineAssets: true,
      reportDir: 'reports/e2e',
    },
  },
  e2e: {
    setupNodeEvents(on, config) {
      codeCoverage(on, config);
      cypressMochawesomeReporter(on);
      on('before:browser:launch', softwareWebGL);
      return config;
    },
    specPattern: 'tests/e2e/**/*.{cy,spec}.{js,jsx,ts,tsx}',
    excludeSpecPattern: 'tests/e2e/screenshots/*',
    baseUrl: 'http://localhost:4173',
  },
});
