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

const hub = 'http://localhost:3005';
const replica = 'http://localhost:3105';
const password = 'Correct-Horse-Battery-9';

const deployment = {
  SECRET: 'a-secret-that-the-deployment-sets-1234',
  PASSWORD_SALT: 'a-salt-that-the-deployment-sets-5678',
  HTTP_CANONICAL_HOST: hub,
  NETWORK_NAME: 'Token Net',
  DMR_DISABLE_RADIO_ID_VALIDATION: 'true',
};

const bearer = (base: string, method: string, path: string, secret: string) =>
  cy.request({ method, url: `${base}${path}`, headers: { Authorization: `Bearer ${secret}` }, failOnStatusCode: false, body: method === 'GET' ? undefined : {} });

const secretShown = () => cy.get('[data-test="token-secret"] input').invoke('val').then((v) => String(v));

describe('API tokens on two replicas', () => {
  before(() => {
    cy.task('placeholderStart');
    cy.visit(hub);
    cy.task('placeholderStop');
  });

  after(() => {
    cy.task('hubStopAll', null, { timeout: 60000 });
    cy.task('stopBackends', null, { timeout: 60000 });
  });

  it('makes, uses, rotates and deletes tokens, and a full access token stays inside the rights of its user', () => {
    cy.task<Record<string, string>>('backends', null, { timeout: 180000 }).then((backends) => {
      cy.task<string>('workdir', 'tokens-a').then((dirA) => {
        cy.task<{ wizard: boolean }>('hubStart', { name: 'a', dir: dirA, env: { ...deployment, ...backends, DMR_MMDVM_PORT: '62131' } }, { timeout: 120000 }).then(({ wizard }) => {
          expect(wizard, 'the first admin page must open').to.eq(true);
          cy.visit(hub);
          cy.location('pathname', { timeout: 20000 }).should('eq', '/setup/user');
          cy.get('#dmr_id').type('3002068');
          cy.get('#username').type('admin');
          cy.get('#callsign').type('N6CALL');
          cy.get('#password').type(password);
          cy.get('#confirmPassword').type(password);
          cy.contains('button', 'Create User').click();
          cy.task('hubWait', { name: 'a', pattern: 'Server ready to accept traffic' }, { timeout: 120000 });
        });
      });
      cy.task<string>('workdir', 'tokens-b').then((dirB) => {
        cy.task<{ wizard: boolean }>('hubStart', { name: 'b', dir: dirB, env: { ...deployment, ...backends, HTTP_PORT: '3105', DMR_MMDVM_PORT: '62231' } }, { timeout: 120000 })
          .its('wizard').should('eq', false);
      });
    });

    cy.request({ method: 'POST', url: `${hub}/api/v1/auth/login`, body: { username: 'admin', password } }).its('status').should('eq', 200);
    cy.visit(`${hub}/settings`);
    cy.get('[data-test="tokens-card"]').should('contain.text', 'No tokens yet.');

    cy.get('[data-test="new-token"]').click();
    cy.get('#token-name').type('Net logger');
    cy.contains('label', 'See last heard for your radio IDs and your hotspots').find('input').check();
    cy.contains('button', 'Make token').click();
    cy.get('[data-test="token-secret"]').should('contain.text', 'DMRHub shows it only once.');
    secretShown().then((first) => {
      expect(first).to.match(/^dmrhub_pat_[A-Za-z0-9_-]{43}$/);
      bearer(replica, 'GET', '/api/v1/lastheard', first).its('status').should('eq', 200);
      bearer(replica, 'GET', '/api/v1/messages/inbox', first).its('status').should('eq', 403);

      cy.contains('tbody tr', 'Net logger').contains('button', 'Rotate').click();
      cy.get('[data-test="token-secret"]').should('contain.text', 'The old token stopped working.');
      secretShown().then((second) => {
        expect(second).not.to.eq(first);
        bearer(replica, 'GET', '/api/v1/lastheard', first).its('status').should('eq', 401);
        bearer(hub, 'GET', '/api/v1/lastheard', first).its('status').should('eq', 401);
        bearer(replica, 'GET', '/api/v1/lastheard', second).its('status').should('eq', 200);

        cy.contains('tbody tr', 'Net logger').contains('button', 'Delete').click();
        cy.get('[data-test="tokens-card"]').should('contain.text', 'Deleted Net logger. It stopped working.');
        bearer(replica, 'GET', '/api/v1/lastheard', second).its('status').should('eq', 401);
      });
    });

    cy.get('[data-test="new-token"]').click();
    cy.get('#token-name').type('Everything');
    cy.contains('label', 'Full account access').find('input').check();
    cy.get('[data-test="full-access-note"]').should('contain.text', 'It cannot open admin pages, also when you are an admin');
    cy.get('#token-expires').select('Never');
    cy.contains('button', 'Make token').click();
    cy.get('[data-test="token-full"]').should('have.text', 'Full account access');
    secretShown().then((full) => {
      bearer(replica, 'GET', '/api/v1/users/me/settings', full).its('status').should('eq', 200);
      bearer(replica, 'GET', '/api/v1/users/me/radio-ids', full).its('status').should('eq', 200);
      bearer(replica, 'GET', '/api/v1/admin/messages', full).its('status').should('eq', 403);
      bearer(replica, 'GET', '/api/v1/config', full).its('status').should('eq', 403);
      bearer(replica, 'GET', '/api/v1/users/me/tokens', full).its('status').should('eq', 403);
      bearer(replica, 'PUT', '/api/v1/users/me/email', full).its('status').should('eq', 403);
      bearer(replica, 'PATCH', '/api/v1/users/3002068', full).its('status').should('eq', 403);
    });
  });
});
