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

const repeater = {
  id: 310999901,
  callsign: 'N0CALL',
  latitude: 30.2669,
  longitude: -97.7729,
  height: 12,
  location: 'Zilker Park',
  description: 'Test repeater',
  connected_time: new Date(Date.now() - 2 * 3600000).toISOString(),
  last_ping_time: new Date().toISOString(),
};

function stubPositions(repeaters: (typeof repeater)[]) {
  cy.intercept('GET', '/api/v1/users/me/settings', { statusCode: 200, body: { sms_storage: false, share_positions: false } });
  cy.intercept('GET', '/api/v1/positions/radios', {
    statusCode: 200,
    body: { radios: [], intervals: [5, 10, 15, 30, 60], retention_hours: [24, 168, 720], aprs: false, callsign: 'W5TST' },
  });
  cy.intercept({ method: 'GET', pathname: '/api/v1/positions' }, { statusCode: 200, body: { positions: [] } }).as('getPositions');
  cy.intercept('GET', '/api/v1/positions/repeaters', { statusCode: 200, body: { repeaters } }).as('getRepeaters');
}

describe('Positions of repeaters', () => {
  beforeEach(() => {
    cy.stubCommonEndpoints();
    cy.login();
  });

  it('lists a connected repeater that reports its position', () => {
    stubPositions([repeater]);
    cy.visit('/positions');
    cy.wait(['@getPositions', '@getRepeaters']);

    cy.get('[data-test="repeaters"]').should('contain.text', 'Repeaters');
    cy.get('[data-test="repeater-310999901"]')
      .should('contain.text', 'N0CALL · 310999901')
      .and('contain.text', '30.2669, −97.7729')
      .and('contain.text', 'Zilker Park · Test repeater')
      .and('contain.text', '2 hours ago');
    cy.get('[data-test="repeater-310999901"] a').should('have.attr', 'href', '/repeaters/310999901');
    cy.get('[data-test="repeater-310999901"]').click();
    cy.get('[data-test="repeater-310999901"]').should('have.class', 'bg-muted/50');
  });

  it('shows no repeaters when none reports a position', () => {
    stubPositions([]);
    cy.visit('/positions');
    cy.wait(['@getPositions', '@getRepeaters']);

    cy.get('[data-test="no-radios"]').should('be.visible');
    cy.get('[data-test="repeaters"]').should('not.exist');
  });
});
