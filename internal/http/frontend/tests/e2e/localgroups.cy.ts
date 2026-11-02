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

const owner = { id: 1234567, callsign: 'W5TST' };
const group = (repeaters: unknown[]) => ({
  id: 5, name: 'North Texas', talkgroup: 8, owner, members: 1, mine: true, can_manage: true, involved: true, repeaters,
});
const mine = { id: 990388, callsign: 'W5TST', owner, slot: 2, state: 'member', mine: true };
const asked = { id: 300206801, callsign: 'N7CALL', owner: { id: 3002069, callsign: 'N7CALL' }, slot: 2, state: 'asked', mine: false };

describe('Local groups', () => {
  beforeEach(() => {
    cy.stubCommonEndpoints();
    cy.login();
    cy.intercept('GET', '/api/v1/repeaters/my*', { statusCode: 200, body: { total: 0, repeaters: [] } });
    cy.intercept('GET', '/api/v1/talkgroups?limit=none', { statusCode: 200, body: { talkgroups: [] } });
  });

  it('lists the groups, opens one and adds a repeater of another owner', () => {
    cy.intercept('GET', '/api/v1/localgroups', { statusCode: 200, body: { groups: [group([mine])], talkgroups: [8, 9] } }).as('list');
    cy.intercept('GET', '/api/v1/localgroups/5', { statusCode: 200, body: group([mine]) }).as('group');
    cy.intercept('POST', '/api/v1/localgroups/5/repeaters', { statusCode: 200, body: group([mine, asked]) }).as('add');

    cy.visit('/repeaters');
    cy.contains('label', 'Local groups').click();
    cy.wait('@list');
    cy.get('[data-test="local-groups"]').should('contain', 'North Texas').and('contain', 'TG 8').and('contain', 'You');
    cy.contains('a', 'North Texas').click();
    cy.wait('@group');
    cy.contains('A call to TG 8 on one of these repeaters goes to all of them, and nowhere else.');
    cy.get('[data-test="add-repeater"]').type('300206801');
    cy.contains('button', 'Add').click();
    cy.wait('@add').its('request.body').should('deep.equal', { repeater_id: 300206801, slot: 2 });
    cy.get('[data-test="member-300206801"]').should('contain', 'Waiting for the owner');
  });

  it('makes a new group', () => {
    cy.intercept('GET', '/api/v1/localgroups', { statusCode: 200, body: { groups: [], talkgroups: [8, 9] } });
    cy.intercept('POST', '/api/v1/localgroups', { statusCode: 200, body: group([]) }).as('create');
    cy.intercept('GET', '/api/v1/localgroups/5', { statusCode: 200, body: group([]) });

    cy.visit('/repeaters?view=local');
    cy.contains('No local groups yet');
    cy.get('[data-test="new-local-group"]').click();
    cy.get('[data-test="group-name"]').type('North Texas');
    cy.get('[data-test="group-save"]').click();
    cy.wait('@create').its('request.body').should('deep.equal', { name: 'North Texas', talkgroup: 8 });
    cy.location('pathname').should('eq', '/localgroups/5');
    cy.contains('No repeaters yet');
  });

  it('lets the owner of a repeater say yes to a group', () => {
    const repeater = {
      id: 300206801, callsign: 'W5TST', type: 'mmdvm', last_ping_time: '2024-06-10T14:05:00Z', connected_time: '2024-06-10T14:00:00Z',
      created_at: '2024-01-10T09:00:00Z', ts1_static_talkgroups: [], ts2_static_talkgroups: [], ts1_dynamic_talkgroup: { id: 0 },
      ts2_dynamic_talkgroup: { id: 0 }, latitude: 0, longitude: 0, rx_frequency: 0, tx_frequency: 0, tx_power: 0, color_code: 1, owner,
    };
    const ask = { id: 5, name: 'North Texas', talkgroup: 8, slot: 2, owner: { id: 3002070, callsign: 'K1CALL' }, asked_by: { id: 3002070, callsign: 'K1CALL' }, repeaters: 4 };
    cy.intercept('GET', '/api/v1/repeaters/300206801', { statusCode: 200, body: repeater });
    cy.intercept('GET', '/api/v1/repeaters/300206801/rules', { statusCode: 200, body: { ts1_users: [], ts2_users: [], talkgroup_mode: 'off', talkgroups: [] } });
    cy.intercept('GET', '/api/v1/lastheard/repeater/300206801*', { statusCode: 200, body: { calls: [], total: 0 } });
    cy.intercept('GET', '/api/v1/repeaters/300206801/cosysops', { statusCode: 200, body: { owner, role: 'owner', cosysops: [] } });
    cy.intercept('GET', '/api/v1/repeaters/300206801/on-air', { statusCode: 200, body: [] });
    cy.intercept('GET', '/api/v1/repeaters/300206801/events*', { statusCode: 200, body: { events: [], total: 0 } });
    cy.intercept('GET', '/api/v1/repeaters/300206801/alert', { statusCode: 200, body: { enabled: false } });
    let answered = false;
    cy.intercept('GET', '/api/v1/repeaters/300206801/localgroups', (req) => {
      req.reply({ statusCode: 200, body: [{ ...ask, state: answered ? 'member' : 'asked' }] });
    });
    cy.intercept('POST', '/api/v1/localgroups/5/repeaters/300206801/accept', (req) => {
      answered = true;
      req.reply({ statusCode: 200, body: {} });
    }).as('accept');

    cy.visit('/repeaters/300206801');
    cy.contains('K1CALL asks to add this repeater to the local group North Texas.');
    cy.contains('button', 'Add it').click();
    cy.wait('@accept');
    cy.get('[data-test="local-ask-5"]').should('not.exist');
    cy.get('[data-test="ts2-local"]').should('contain', 'TG 8').and('contain', 'North Texas');
  });
});
