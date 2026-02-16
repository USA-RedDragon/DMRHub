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

describe('Navigation & Auth Guards', () => {
  beforeEach(() => {
    cy.stubCommonEndpoints();
  });

  it('unauthenticated user sees limited nav items', () => {
    cy.intercept('GET', '/api/v1/users/me', { statusCode: 401, body: {} });

    cy.visit('/');

    // Should see Home, Last Heard, Register, Login
    cy.get('header nav.hidden.md\\:flex').within(() => {
      cy.contains('Home').should('be.visible');
      cy.contains('Last Heard').should('be.visible');
    });

    // Auth links
    cy.get('header .hidden.md\\:flex').last().within(() => {
      cy.contains('Register').should('be.visible');
      cy.contains('Login').should('be.visible');
    });

    // Should NOT see Repeaters, Talkgroups, Admin
    cy.get('header nav.hidden.md\\:flex').within(() => {
      cy.contains('Repeaters').should('not.exist');
      cy.contains('Admin').should('not.exist');
    });
  });

  it('authenticated non-admin user sees user nav items', () => {
    cy.login();
    cy.visit('/');

    // Should see Repeaters and Talkgroups
    cy.get('header nav.hidden.md\\:flex').within(() => {
      cy.contains('Repeaters').should('be.visible');
      cy.contains('Talkgroups').should('be.visible');
    });

    // Should NOT see Admin dropdown
    cy.get('header nav.hidden.md\\:flex').within(() => {
      cy.contains('Admin').should('not.exist');
    });

    // Should see Logout
    cy.get('header').should('contain.text', 'Logout');
  });

  it('admin user sees Admin dropdown in nav', () => {
    cy.loginAsAdmin();
    cy.visit('/');

    cy.get('header nav.hidden.md\\:flex').within(() => {
      cy.contains('Admin').should('be.visible');
    });
  });

  it('accessing admin page as non-admin gets redirected on 403', () => {
    cy.login();

    // Mock that admin page APIs return 403
    cy.intercept('GET', '/api/v1/users?page=*&limit=*', {
      statusCode: 403,
      body: { error: 'Forbidden' },
    });

    cy.visit('/admin/users');

    // The API 403 interceptor should redirect to /login
    cy.url().should('include', '/login');
  });
});
