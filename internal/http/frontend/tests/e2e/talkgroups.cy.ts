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

describe('Talkgroup CRUD', () => {
  beforeEach(() => {
    cy.stubCommonEndpoints();
    cy.loginAsAdmin();
  });

  it('displays talkgroup list for owner', () => {
    cy.intercept('GET', '/api/v1/users?limit=none', {
      statusCode: 200,
      body: { users: [{ id: 1234567, callsign: 'W5TST' }] },
    });
    cy.intercept('GET', '/api/v1/talkgroups?limit=*&page=*', {
      statusCode: 200,
      fixture: 'talkgroups.json',
    }).as('getTalkgroups');
    cy.intercept('GET', '/api/v1/talkgroups/my*', {
      statusCode: 200,
      fixture: 'talkgroups.json',
    });

    cy.visit('/talkgroups');
    cy.wait('@getTalkgroups');

    cy.contains('World').should('be.visible');
    cy.contains('Local').should('be.visible');
  });

  it('deletes a talkgroup and removes it from table (admin)', () => {
    const talkgroupsData = {
      total: 2,
      talkgroups: [
        {
          id: 1,
          name: 'World',
          description: 'Worldwide talkgroup',
          admins: [],
          ncos: [],
          created_at: '2024-01-01T00:00:00Z',
        },
        {
          id: 2,
          name: 'Local',
          description: 'Local talkgroup',
          admins: [],
          ncos: [],
          created_at: '2024-01-02T00:00:00Z',
        },
      ],
    };

    const afterDeleteData = {
      total: 1,
      talkgroups: [talkgroupsData.talkgroups[1]],
    };

    cy.intercept('GET', '/api/v1/users?limit=none', {
      statusCode: 200,
      body: { users: [{ id: 1234567, callsign: 'W5TST' }] },
    });

    cy.intercept('GET', '/api/v1/talkgroups?limit=*&page=*', {
      statusCode: 200,
      body: talkgroupsData,
    }).as('getTalkgroups');

    cy.visit('/admin/talkgroups');
    cy.wait('@getTalkgroups');
    cy.contains('World').should('be.visible');

    // Set up delete and re-fetch intercepts
    cy.intercept('DELETE', '/api/v1/talkgroups/1', {
      statusCode: 200,
      body: {},
    }).as('deleteTalkgroup');

    cy.intercept('GET', '/api/v1/talkgroups?limit=*&page=*', {
      statusCode: 200,
      body: afterDeleteData,
    }).as('getTalkgroupsAfterDelete');

    cy.on('window:confirm', () => true);

    // Admin talkgroup table shows Delete next to Edit (not inside edit mode)
    cy.contains('tr', 'World').find('button').contains('Delete').click();

    cy.wait('@deleteTalkgroup');

    // The deleted talkgroup should be gone
    cy.contains('td', 'World').should('not.exist');
    cy.contains('Local').should('be.visible');
  });

  it('edits talkgroup name and saves', () => {
    cy.intercept('GET', '/api/v1/users?limit=none', {
      statusCode: 200,
      body: { users: [{ id: 1234567, callsign: 'W5TST' }] },
    });
    cy.intercept('GET', '/api/v1/talkgroups?limit=*&page=*', {
      statusCode: 200,
      fixture: 'talkgroups.json',
    }).as('getTalkgroups');

    cy.intercept('PATCH', '/api/v1/talkgroups/1', {
      statusCode: 200,
      body: {},
    }).as('patchTalkgroup');

    cy.intercept('POST', '/api/v1/talkgroups/1/admins', {
      statusCode: 200,
      body: {},
    }).as('postAdmins');

    cy.intercept('POST', '/api/v1/talkgroups/1/ncos', {
      statusCode: 200,
      body: {},
    }).as('postNCOs');

    cy.visit('/admin/talkgroups');
    cy.wait('@getTalkgroups');

    // Click Edit on first talkgroup
    cy.contains('tr', 'World').find('button').contains('Edit').click();

    // After Edit, 'World' is in an input value, so use Save button directly
    cy.contains('button', 'Save').click();

    cy.wait('@patchTalkgroup');
    cy.wait('@postAdmins');
    cy.wait('@postNCOs');
  });

  it('creates a new talkgroup (admin)', () => {
    cy.intercept('GET', '/api/v1/users?limit=none', {
      statusCode: 200,
      body: { users: [{ id: 1234567, callsign: 'W5TST', display: '1234567 - W5TST' }] },
    });

    cy.intercept('POST', '/api/v1/talkgroups', {
      statusCode: 200,
      body: {},
    }).as('createTalkgroup');

    cy.intercept('POST', '/api/v1/talkgroups/100/admins', {
      statusCode: 200,
      body: {},
    }).as('setAdmins');

    cy.intercept('POST', '/api/v1/talkgroups/100/ncos', {
      statusCode: 200,
      body: {},
    }).as('setNCOs');

    cy.visit('/admin/talkgroups/new');

    cy.get('#id').type('100');
    cy.get('#name').type('Test TG');
    cy.get('#description').type('A test talkgroup');
    cy.get('button[type="submit"]').click();

    cy.wait('@createTalkgroup');
    cy.wait('@setAdmins');
    cy.wait('@setNCOs');
  });

  it('shows validation errors on new talkgroup form', () => {
    cy.intercept('GET', '/api/v1/users?limit=none', {
      statusCode: 200,
      body: { users: [] },
    });

    cy.visit('/admin/talkgroups/new');

    // Wait for the form to fully render before submitting
    cy.get('#id').should('be.visible');
    cy.get('button[type="submit"]').click();

    // Should show validation errors (aria-invalid is set via
    // $invalid && submitted, which is more reliable in production builds)
    cy.get('#id').should('have.attr', 'aria-invalid', 'true');
  });
});
