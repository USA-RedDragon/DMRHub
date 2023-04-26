/* eslint-disable no-undef */
const { defineConfig } = require('cypress');
const process = require('process');

module.exports = defineConfig({
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
      require('cypress-mochawesome-reporter/plugin')(on);
      return config;
    },
    specPattern: 'tests/e2e/**/*.{cy,spec}.{js,jsx,ts,tsx}',
    excludeSpecPattern: 'tests/e2e/screenshots/*',
    baseUrl: 'http://localhost:4173',
  },
});
