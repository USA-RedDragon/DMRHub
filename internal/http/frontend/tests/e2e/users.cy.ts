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

describe('User Management', () => {
  beforeEach(() => {
    cy.stubCommonEndpoints();
    cy.loginAsAdmin();
  });

  it('displays users list as admin', () => {
    cy.intercept('GET', '/api/v1/users?page=*&limit=*', {
      statusCode: 200,
      fixture: 'users.json',
    }).as('getUsers');

    cy.visit('/admin/users');
    cy.wait('@getUsers');

    cy.contains('W5TST').should('be.visible');
    cy.contains('N0CALL').should('be.visible');
  });

  it('deletes a user as super admin', () => {
    const usersData = {
      total: 2,
      users: [
        {
          id: 1234567, callsign: 'W5TST', username: 'testuser',
          approved: true, suspended: false, admin: false,
          repeaters: [], created_at: '2024-01-15T10:30:00Z',
        },
        {
          id: 7654321, callsign: 'N0CALL', username: 'anotheruser',
          approved: true, suspended: false, admin: false,
          repeaters: [{ id: 311001 }], created_at: '2024-02-20T08:00:00Z',
        },
      ],
    };

    const afterDeleteData = {
      total: 1,
      users: [usersData.users[1]],
    };

    cy.intercept('GET', '/api/v1/users?page=*&limit=*', {
      statusCode: 200,
      body: usersData,
    }).as('getUsers');

    cy.visit('/admin/users');
    cy.wait('@getUsers');

    // Set up delete intercept
    cy.intercept('DELETE', '/api/v1/users/1234567', {
      statusCode: 200,
      body: {},
    }).as('deleteUser');

    cy.intercept('GET', '/api/v1/users?page=*&limit=*', {
      statusCode: 200,
      body: afterDeleteData,
    }).as('getUsersAfterDelete');

    cy.on('window:confirm', () => true);

    // Click Delete button for first user
    cy.contains('tr', 'W5TST').find('button').contains('Delete').click();

    cy.wait('@deleteUser');

    // Deleted user should be gone
    cy.contains('td', 'W5TST').should('not.exist');
    cy.contains('N0CALL').should('be.visible');
  });

  it('suspends a user via checkbox', () => {
    cy.intercept('GET', '/api/v1/users?page=*&limit=*', {
      statusCode: 200,
      fixture: 'users.json',
    }).as('getUsers');

    cy.intercept('POST', '/api/v1/users/suspend/1234567', {
      statusCode: 200,
      body: {},
    }).as('suspendUser');

    cy.visit('/admin/users');
    cy.wait('@getUsers');

    // Find the suspend checkbox for the first user and check it
    cy.contains('tr', 'W5TST').find('input[type="checkbox"]').first().check({ force: true });

    cy.wait('@suspendUser');
  });

  it('approves a user from approval page', () => {
    cy.intercept('GET', '/api/v1/users/unapproved*', {
      statusCode: 200,
      fixture: 'unapproved-users.json',
    }).as('getUnapproved');

    cy.intercept('POST', '/api/v1/users/approve/5555555', {
      statusCode: 200,
      body: {},
    }).as('approveUser');

    cy.on('window:confirm', () => true);

    cy.visit('/admin/users/approval');
    cy.wait('@getUnapproved');

    cy.contains('KB0NEW').should('be.visible');

    // Click Approve button
    cy.contains('tr', 'KB0NEW').find('button').contains('Approve').click();

    cy.wait('@approveUser');
  });

  it('edits user callsign and saves', () => {
    cy.intercept('GET', '/api/v1/users?page=*&limit=*', {
      statusCode: 200,
      fixture: 'users.json',
    }).as('getUsers');

    cy.intercept('PATCH', '/api/v1/users/1234567', {
      statusCode: 200,
      body: {},
    }).as('patchUser');

    cy.visit('/admin/users');
    cy.wait('@getUsers');

    // Click Edit — row found by callsign (still plain text)
    cy.contains('tr', '1234567').find('button').contains('Edit').click();

    cy.contains('tr', '1234567').find('button').contains('Save Changes').click();

    cy.wait('@patchUser');
  });
});
