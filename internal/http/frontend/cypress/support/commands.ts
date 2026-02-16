// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2026 Jacob McSwain
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

declare namespace Cypress {
  interface Chainable {
    /**
     * Log in as a regular user. Stubs auth endpoints.
     */
    login(): Chainable<void>;

    /**
     * Log in as a super admin. Stubs auth endpoints.
     */
    loginAsAdmin(): Chainable<void>;

    /**
     * Shorthand for cy.intercept() with the /api/v1 prefix.
     */
    mockAPI(
      method: string,
      path: string,
      response: object | string | number,
      statusCode?: number,
    ): Chainable<null>;

    /**
     * Stub common endpoints called on every page (network name, version, setupwizard).
     */
    stubCommonEndpoints(): Chainable<void>;
  }
}

Cypress.Commands.add('mockAPI', (method, path, response, statusCode = 200) => {
  cy.intercept(method as Cypress.HttpMethod, `/api/v1${path}`, {
    statusCode,
    body: response,
  });
});

Cypress.Commands.add('stubCommonEndpoints', () => {
  cy.intercept('GET', '/api/v1/setupwizard', {
    statusCode: 200,
    body: { setupwizard: false },
  });
  cy.intercept('GET', '/api/v1/network/name', {
    statusCode: 200,
    body: 'TestNetwork',
  });
  cy.intercept('GET', '/api/v1/version', {
    statusCode: 200,
    body: '1.0.0-test',
  });
  // Stub WebSocket upgrade requests to prevent connection errors
  cy.intercept('GET', '/ws/*', { statusCode: 404 });
});

Cypress.Commands.add('login', () => {
  cy.fixture('user.json').then((user) => {
    cy.intercept('POST', '/api/v1/auth/login', {
      statusCode: 200,
      body: { message: 'Logged in' },
    });
    cy.intercept('GET', '/api/v1/users/me', {
      statusCode: 200,
      body: user,
    });
  });
});

Cypress.Commands.add('loginAsAdmin', () => {
  cy.fixture('admin-user.json').then((admin) => {
    cy.intercept('POST', '/api/v1/auth/login', {
      statusCode: 200,
      body: { message: 'Logged in' },
    });
    cy.intercept('GET', '/api/v1/users/me', {
      statusCode: 200,
      body: admin,
    });
  });
});
