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

describe('Repeater CRUD', () => {
  beforeEach(() => {
    cy.stubCommonEndpoints();
    cy.login();
  });

  it('displays repeater list with mocked data', () => {
    cy.intercept('GET', '/api/v1/talkgroups?limit=none', {
      statusCode: 200,
      body: { talkgroups: [{ id: 1, name: 'World' }, { id: 2, name: 'Local' }] },
    });
    cy.intercept('GET', '/api/v1/repeaters/my*', {
      statusCode: 200,
      fixture: 'repeaters.json',
    }).as('getRepeaters');

    cy.visit('/repeaters');
    cy.wait('@getRepeaters');

    // Table should render with repeater data
    cy.contains('311001').should('be.visible');
    cy.contains('311002').should('be.visible');
  });

  it('deletes a repeater and removes it from the table (regression test)', () => {
    const repeatersData = {
      total: 2,
      repeaters: [
        {
          id: 311001,
          type: 'mmdvm',
          connected_time: '2024-06-10T14:00:00Z',
          created_at: '2024-01-10T09:00:00Z',
          last_ping_time: '2024-06-10T14:05:00Z',
          slots: 2,
          ts1_static_talkgroups: [],
          ts2_static_talkgroups: [],
          ts1_dynamic_talkgroup: { id: 0, name: 'None' },
          ts2_dynamic_talkgroup: { id: 0, name: 'None' },
          hotspot: true,
          simplex_repeater: false,
        },
        {
          id: 311002,
          type: 'mmdvm',
          connected_time: '2024-06-09T12:00:00Z',
          created_at: '2024-02-20T11:00:00Z',
          last_ping_time: '2024-06-09T12:05:00Z',
          slots: 2,
          ts1_static_talkgroups: [],
          ts2_static_talkgroups: [],
          ts1_dynamic_talkgroup: { id: 0, name: 'None' },
          ts2_dynamic_talkgroup: { id: 0, name: 'None' },
          hotspot: false,
          simplex_repeater: false,
        },
      ],
    };

    const afterDeleteData = {
      total: 1,
      repeaters: [repeatersData.repeaters[1]],
    };

    cy.intercept('GET', '/api/v1/talkgroups?limit=none', {
      statusCode: 200,
      body: { talkgroups: [{ id: 1, name: 'World' }, { id: 2, name: 'Local' }] },
    });

    // Initial load
    cy.intercept('GET', '/api/v1/repeaters/my*', {
      statusCode: 200,
      body: repeatersData,
    }).as('getRepeaters');

    cy.visit('/repeaters');
    cy.wait('@getRepeaters');
    cy.contains('311001').should('be.visible');

    // Click Edit on the first repeater to enter edit mode (Delete is only visible in edit mode)
    cy.contains('tr', '311001').find('button').contains('Edit').click();

    // Set up delete and re-fetch intercepts
    cy.intercept('DELETE', '/api/v1/repeaters/311001', {
      statusCode: 200,
      body: {},
    }).as('deleteRepeater');

    // After delete, the re-fetch should return only the second repeater
    cy.intercept('GET', '/api/v1/repeaters/my*', {
      statusCode: 200,
      body: afterDeleteData,
    }).as('getRepeatersAfterDelete');

    // Stub window.confirm for the confirmation dialog
    cy.on('window:confirm', () => true);

    // Click Delete
    cy.contains('tr', '311001').find('button').contains('Delete').click();

    cy.wait('@deleteRepeater');

    // The deleted repeater should be gone from the table
    cy.get('table').should('not.contain', '311001');
    cy.get('table').contains('311002').should('be.visible');
  });

  it('edits repeater talkgroups and saves', () => {
    cy.intercept('GET', '/api/v1/talkgroups?limit=none', {
      statusCode: 200,
      body: { talkgroups: [{ id: 1, name: 'World' }, { id: 2, name: 'Local' }] },
    });

    cy.intercept('GET', '/api/v1/repeaters/my*', {
      statusCode: 200,
      fixture: 'repeaters.json',
    }).as('getRepeaters');

    cy.intercept('POST', '/api/v1/repeaters/311001/talkgroups', {
      statusCode: 200,
      body: {},
    }).as('saveTalkgroups');

    cy.intercept('PATCH', '/api/v1/repeaters/311001', {
      statusCode: 200,
      body: {},
    }).as('patchRepeater');

    cy.visit('/repeaters');
    cy.wait('@getRepeaters');

    // Click Edit on first repeater
    cy.contains('tr', '311001').find('button').contains('Edit').click();

    // Click Save
    cy.contains('tr', '311001').find('button').contains('Save').click();

    cy.wait('@saveTalkgroups');
    cy.wait('@patchRepeater');
  });

  it('cancels edit and restores original data', () => {
    cy.intercept('GET', '/api/v1/talkgroups?limit=none', {
      statusCode: 200,
      body: { talkgroups: [{ id: 1, name: 'World' }, { id: 2, name: 'Local' }] },
    });

    cy.intercept('GET', '/api/v1/repeaters/my*', {
      statusCode: 200,
      fixture: 'repeaters.json',
    }).as('getRepeaters');

    cy.visit('/repeaters');
    cy.wait('@getRepeaters');

    // Click Edit
    cy.contains('tr', '311001').find('button').contains('Edit').click();

    // Verify Cancel button is visible
    cy.contains('tr', '311001').find('button').contains('Cancel').should('be.visible');

    // Click Cancel
    cy.contains('tr', '311001').find('button').contains('Cancel').click();

    // Should go back to view mode with Edit button
    cy.contains('tr', '311001').find('button').contains('Edit').should('be.visible');
  });

  it('enrolls a new repeater', () => {
    cy.intercept('POST', '/api/v1/repeaters', {
      statusCode: 200,
      body: { password: 'test-secret-password' },
    }).as('createRepeater');

    // Stub window.alert
    const alertStub = cy.stub();
    cy.on('window:alert', alertStub);

    cy.visit('/repeaters/new');

    cy.get('#radioID').type('311099');
    cy.get('button[type="submit"]').click();

    cy.wait('@createRepeater').then(() => {
      expect(alertStub).to.have.been.calledOnce;
      // Alert should contain the password
      const alertText = alertStub.firstCall.args[0];
      expect(alertText).to.contain('test-secret-password');
    });
  });

  it('shows validation errors on empty new repeater form', () => {
    cy.visit('/repeaters/new');

    // Wait for the form to fully render before submitting
    cy.get('#radioID').should('be.visible');
    cy.get('button[type="submit"]').click();

    // Should show validation error for Radio ID (aria-invalid is set via
    // $invalid && submitted, which is more reliable in production builds)
    cy.get('#radioID').should('have.attr', 'aria-invalid', 'true');
  });
});
