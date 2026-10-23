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

describe('Admin Setup: OpenBridge', () => {
  beforeEach(() => {
    cy.stubCommonEndpoints();
    cy.loginAsAdmin();
    cy.mockAPI('GET', '/network/features', { openbridge: true, ipsc: true });
    cy.mockAPI('GET', '/config', {
      dmr: {
        'radio-id-url': 'https://radioid.net/static/users.json',
        mmdvm: { bind: '[::]', port: 62031 },
        ipsc: { enabled: true, bind: '[::]', port: 50000, 'network-id': 311860 },
        openbridge: { enabled: true, bind: '[::]', port: 62035 },
        retention: { messages: 0, transmissions: 0, bursts: 0 },
      },
      http: {}, database: {}, smtp: {}, redis: {}, metrics: {}, pprof: {},
      secrets: { secretSet: true, passwordSaltSet: true, smtpPasswordSet: false },
    }).as('config');
    cy.mockAPI('POST', '/config/validate', { valid: true, errors: {} }).as('validate');
    cy.mockAPI('PUT', '/config', { message: 'Configuration updated successfully' }).as('save');
  });

  it('shows the Server ID field and saves it', () => {
    cy.visit('/admin/setup');
    cy.wait('@config');
    cy.contains('OpenBridge Settings').should('be.visible');
    cy.contains('label', 'Server ID').should('be.visible');
    cy.get('#openbridge-server-id').should('be.visible').clear();
    cy.get('#openbridge-server-id').type('3129');
    cy.contains('button', 'Save').click();
    cy.wait('@validate');
    cy.wait('@save').its('request.body.dmr.openbridge').should('deep.equal', {
      enabled: true, bind: '[::]', port: 62035, 'server-id': 3129,
    });
  });

  it('turns OpenBridge on from its own Enabled box', () => {
    cy.mockAPI('GET', '/config', {
      dmr: { ipsc: { enabled: true }, openbridge: { enabled: false } },
      http: {}, database: {}, smtp: {}, redis: {}, metrics: {}, pprof: {},
    }).as('config');
    cy.visit('/admin/setup');
    cy.wait('@config');
    cy.get('#openbridge-server-id').should('not.exist');
    cy.get('label[for="openbridge-enabled"]').click();
    cy.get('#openbridge-server-id').should('be.visible');
    cy.get('#enabled').should('be.checked');
  });
});
