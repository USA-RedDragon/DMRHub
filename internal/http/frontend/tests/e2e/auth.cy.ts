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

describe('Auth Flow', () => {
  beforeEach(() => {
    cy.stubCommonEndpoints();
  });

  it('logs in with valid credentials and redirects to home', () => {
    // Start unauthenticated
    cy.intercept('GET', '/api/v1/users/me', { statusCode: 401, body: {} });
    cy.visit('/login');

    // Now set up the login intercepts
    cy.login();

    cy.get('#username').type('testuser');
    cy.get('#password').type('password123');
    cy.get('button[type="submit"]').click();

    // After login, should redirect to /
    cy.url().should('eq', Cypress.config().baseUrl + '/');

    // Header should show Logout link (user is logged in)
    cy.get('header').should('contain.text', 'Logout');
  });

  it('shows error on invalid credentials', () => {
    cy.intercept('GET', '/api/v1/users/me', { statusCode: 401, body: {} });
    cy.intercept('POST', '/api/v1/auth/login', {
      statusCode: 401,
      body: { error: 'Invalid username or password' },
    });

    cy.visit('/login');

    cy.get('#username').type('baduser');
    cy.get('#password').type('wrongpass');
    cy.get('button[type="submit"]').click();

    // Should stay on /login
    cy.url().should('include', '/login');
  });

  it('shows validation errors on empty form submission', () => {
    cy.intercept('GET', '/api/v1/users/me', { statusCode: 401, body: {} });
    cy.visit('/login');

    // Wait for the form to fully render before submitting
    cy.get('#username').should('be.visible');
    cy.get('#password').should('be.visible');
    cy.get('button[type="submit"]').click();

    // Validation errors should appear (aria-invalid is set via $invalid && submitted,
    // which is more reliable in production builds than $error which requires $dirty)
    cy.get('#username').should('have.attr', 'aria-invalid', 'true');
    cy.get('#password').should('have.attr', 'aria-invalid', 'true');
  });

  it('logs out and redirects to login', () => {
    cy.login();
    cy.intercept('GET', '/api/v1/auth/logout', {
      statusCode: 200,
      body: { message: 'Logged out' },
    });

    cy.visit('/');

    // Wait for the user state to populate
    cy.get('header').should('contain.text', 'Logout');

    // Click logout (desktop nav)
    cy.get('header .hidden.md\\:flex a').contains('Logout').click();

    // Should redirect to /login
    cy.url().should('include', '/login');

    // Nav should revert to unauthenticated
    cy.get('header').should('contain.text', 'Login');
  });

  it('redirects to /login on session expiry (401 on protected route)', () => {
    // Initially authenticated
    cy.login();
    cy.visit('/');
    cy.get('header').should('contain.text', 'Logout');

    // Now simulate session expiry: /users/me returns 401
    cy.intercept('GET', '/api/v1/users/me', { statusCode: 401, body: {} });

    // Navigating to a protected page that makes an API call returning 401
    // The API interceptor should redirect to /login
    cy.intercept('GET', '/api/v1/repeaters/my*', {
      statusCode: 401,
      body: { error: 'Unauthorized' },
    });
    cy.intercept('GET', '/api/v1/talkgroups?limit=none', {
      statusCode: 401,
      body: { error: 'Unauthorized' },
    });

    cy.visit('/repeaters');

    // Should redirect to /login due to 401 interceptor
    cy.url().should('include', '/login');
  });
});
