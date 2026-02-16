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

describe('Registration Flow', () => {
  beforeEach(() => {
    cy.stubCommonEndpoints();
    // Unauthenticated
    cy.intercept('GET', '/api/v1/users/me', { statusCode: 401, body: {} });
  });

  it('registers a new user successfully', () => {
    cy.intercept('POST', '/api/v1/users', {
      statusCode: 200,
      body: { message: 'Registration successful. Please wait for admin approval.' },
    }).as('registerUser');

    cy.visit('/register');

    cy.get('#dmr_id').type('9999999');
    cy.get('#username').type('newham');
    cy.get('#callsign').type('KD0NEW');
    cy.get('#password').type('SecurePass123');
    cy.get('#confirmPassword').type('SecurePass123');
    cy.get('button[type="submit"]').click();

    cy.wait('@registerUser').its('request.body').should('deep.include', {
      id: 9999999,
      callsign: 'KD0NEW',
      username: 'newham',
      password: 'SecurePass123',
    });
  });

  it('shows validation errors with empty registration form', () => {
    cy.visit('/register');

    // Wait for the form to fully render before submitting
    cy.get('#dmr_id').should('be.visible');
    cy.get('button[type="submit"]').click();

    // Should show validation errors for required fields (aria-invalid is set via
    // $invalid && submitted, which is more reliable in production builds than
    // $error which requires $dirty from $touch())
    cy.get('#dmr_id').should('have.attr', 'aria-invalid', 'true');
  });

  it('shows validation error for mismatched passwords', () => {
    cy.visit('/register');

    // Wait for the form to fully render
    cy.get('#dmr_id').should('be.visible');
    cy.get('#dmr_id').type('9999999');
    cy.get('#username').type('newham');
    cy.get('#callsign').type('KD0NEW');
    cy.get('#password').type('SecurePass123');
    cy.get('#confirmPassword').type('DifferentPass456');
    cy.get('button[type="submit"]').click();

    // The sameAs validator should trigger an error on confirmPassword
    cy.get('#confirmPassword').should('have.attr', 'aria-invalid', 'true');
  });

  it('shows validation error for non-numeric DMR ID', () => {
    cy.visit('/register');

    // Wait for the form to fully render
    cy.get('#dmr_id').should('be.visible');
    cy.get('#dmr_id').type('notanumber');
    cy.get('#username').type('newham');
    cy.get('#callsign').type('KD0NEW');
    cy.get('#password').type('SecurePass123');
    cy.get('#confirmPassword').type('SecurePass123');
    cy.get('button[type="submit"]').click();

    // The numeric validator should trigger an error on dmr_id
    cy.get('#dmr_id').should('have.attr', 'aria-invalid', 'true');
  });
});
