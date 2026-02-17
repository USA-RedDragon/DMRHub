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

describe('Feature Gating: IPSC & OpenBridge', () => {
  describe('OpenBridge enabled', () => {
    beforeEach(() => {
      cy.stubCommonEndpoints();
      cy.intercept('GET', '/api/v1/config', {
        statusCode: 200,
        body: {
          dmr: {
            openbridge: { enabled: true },
            ipsc: { enabled: false },
          },
        },
      });
    });

    it('authenticated user sees OpenBridge Peers nav link', () => {
      cy.login();
      cy.visit('/');

      cy.get('header nav.hidden.md\\:flex').within(() => {
        cy.contains('OpenBridge Peers').should('be.visible');
      });
    });

    it('admin user sees Peers item in Admin dropdown', () => {
      cy.loginAsAdmin();
      cy.visit('/');

      cy.get('header nav.hidden.md\\:flex').within(() => {
        // Open the Admin dropdown
        cy.contains('Admin').click();
      });

      // Dropdown content renders outside the nav, check the whole page
      cy.contains('Peers').should('be.visible');
    });
  });

  describe('OpenBridge disabled', () => {
    beforeEach(() => {
      cy.stubCommonEndpoints();
      cy.intercept('GET', '/api/v1/config', {
        statusCode: 200,
        body: {
          dmr: {
            openbridge: { enabled: false },
            ipsc: { enabled: false },
          },
        },
      });
    });

    it('authenticated user does NOT see OpenBridge Peers nav link', () => {
      cy.login();
      cy.visit('/');

      cy.get('header nav.hidden.md\\:flex').within(() => {
        cy.contains('OpenBridge Peers').should('not.exist');
      });
    });

    it('admin user does NOT see Peers item in Admin dropdown', () => {
      cy.loginAsAdmin();
      cy.visit('/');

      cy.get('header nav.hidden.md\\:flex').within(() => {
        cy.contains('Admin').click();
      });

      // The dropdown should not contain a Peers link to /admin/peers
      cy.get('a[href="/admin/peers"]').should('not.exist');
    });
  });

  describe('IPSC enabled', () => {
    beforeEach(() => {
      cy.stubCommonEndpoints();
      cy.intercept('GET', '/api/v1/config', {
        statusCode: 200,
        body: {
          dmr: {
            openbridge: { enabled: false },
            ipsc: { enabled: true },
          },
        },
      });
    });

    it('shows repeater type dropdown with IPSC option on new repeater page', () => {
      cy.login();

      // Stub the repeater POST endpoint so the page can load without errors
      cy.intercept('GET', '/api/v1/repeaters/my?page=*&limit=*', {
        statusCode: 200,
        body: { total: 0, repeaters: [] },
      });

      cy.visit('/repeaters/new');

      // The type dropdown should be visible with both MMDVM and IPSC options
      cy.get('select#repeaterType').should('be.visible');
      cy.get('select#repeaterType option').should('have.length', 2);
      cy.get('select#repeaterType').contains('MMDVM');
      cy.get('select#repeaterType').contains('Motorola IPSC');
    });
  });

  describe('IPSC disabled', () => {
    beforeEach(() => {
      cy.stubCommonEndpoints();
      cy.intercept('GET', '/api/v1/config', {
        statusCode: 200,
        body: {
          dmr: {
            openbridge: { enabled: false },
            ipsc: { enabled: false },
          },
        },
      });
    });

    it('hides repeater type dropdown when only MMDVM is available', () => {
      cy.login();

      cy.intercept('GET', '/api/v1/repeaters/my?page=*&limit=*', {
        statusCode: 200,
        body: { total: 0, repeaters: [] },
      });

      cy.visit('/repeaters/new');

      // The type dropdown should not exist since there's only one option
      cy.get('select#repeaterType').should('not.exist');
    });
  });
});
