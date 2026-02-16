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

describe('Last Heard & Main Page', () => {
  beforeEach(() => {
    cy.stubCommonEndpoints();
    cy.intercept('GET', '/api/v1/users/me', { statusCode: 401, body: {} });
  });

  it('renders last heard table with mocked data', () => {
    cy.intercept('GET', '/api/v1/lastheard*', {
      statusCode: 200,
      fixture: 'lastheard.json',
    }).as('getLastHeard');

    cy.visit('/lastheard');
    cy.wait('@getLastHeard');

    // Should show the call data
    cy.contains('W5TST').should('be.visible');
    cy.contains('N0CALL').should('be.visible');
    cy.contains('TG 1').should('be.visible');
    // Active call should show "Active"
    cy.contains('Active').should('be.visible');
  });

  it('displays duration, loss, BER, jitter, RSSI formatted correctly', () => {
    cy.intercept('GET', '/api/v1/lastheard*', {
      statusCode: 200,
      fixture: 'lastheard.json',
    }).as('getLastHeard');

    cy.visit('/lastheard');
    cy.wait('@getLastHeard');

    // duration: 5500000000 ns => 5.5s
    cy.contains('5.5s').should('be.visible');
    // loss: 0.005 => 0.5%
    cy.contains('0.5%').should('be.visible');
    // BER: 0.012 => 1.2%
    cy.contains('1.2%').should('be.visible');
    // jitter: 2.3 => 2.3ms
    cy.contains('2.3ms').should('be.visible');
    // RSSI: 45 => -45dBm
    cy.contains('-45dBm').should('be.visible');
  });

  it('last heard pagination triggers new API calls', () => {
    const page1Calls = [];
    for (let i = 0; i < 30; i++) {
      page1Calls.push({
        id: i + 1,
        active: false,
        start_time: '2024-06-10T14:00:00Z',
        time_slot: false,
        user: { id: 1234567, callsign: 'W5TST' },
        is_to_talkgroup: true,
        to_talkgroup: { id: 1 },
        is_to_repeater: false,
        to_repeater: { id: 0, callsign: '' },
        is_to_user: false,
        to_user: { id: 0, callsign: '' },
        duration: 5000000000,
        ber: 0.01,
        loss: 0.005,
        jitter: 2.0,
        rssi: 40,
      });
    }
    const page1Data = {
      total: 60,
      calls: page1Calls,
    };

    cy.intercept('GET', '/api/v1/lastheard?page=1&limit=30', {
      statusCode: 200,
      body: page1Data,
    }).as('page1');

    cy.intercept('GET', '/api/v1/lastheard?page=2&limit=30', {
      statusCode: 200,
      body: { total: 60, calls: [] },
    }).as('page2');

    cy.visit('/lastheard');
    cy.wait('@page1');

    // Click next page button
    cy.contains('button', 'Next').click();
    cy.wait('@page2');
  });

  it('main page renders with network name and version', () => {
    cy.visit('/');

    // Header should contain network name
    cy.get('header').should('contain.text', 'TestNetwork');

    // Footer should contain version
    cy.contains('Version 1.0.0-test').should('be.visible');

    // Main content should show welcome
    cy.contains('Welcome to DMRHub').should('be.visible');
  });
});
