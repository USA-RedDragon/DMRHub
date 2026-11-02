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
  NETWORK_NAME: 'Code Net',
  DMR_DISABLE_RADIO_ID_VALIDATION: 'true',
};

const code = (key: string, steps: number) => cy.task<string>('totp', { key, steps });

describe('On-air code on two replicas', () => {
  before(() => {
    cy.task('placeholderStart');
    cy.visit(hub);
    cy.task('placeholderStop');
  });

  after(() => {
    cy.task('hubStopAll', null, { timeout: 60000 });
    cy.task('stopBackends', null, { timeout: 60000 });
  });

  it('turns on with a code on one replica, and each code works once on both', () => {
    cy.task<Record<string, string>>('backends', null, { timeout: 180000 }).then((backends) => {
      cy.task<string>('workdir', 'onair-a').then((dirA) => {
        cy.task<{ wizard: boolean }>('hubStart', { name: 'a', dir: dirA, env: { ...deployment, ...backends, DMR_MMDVM_PORT: '62141' } }, { timeout: 120000 }).then(({ wizard }) => {
          expect(wizard, 'the first admin page must open').to.eq(true);
          cy.visit(hub);
          cy.location('pathname', { timeout: 20000 }).should('eq', '/setup/user');
          cy.get('#dmr_id').type('3191868');
          cy.get('#username').type('admin');
          cy.get('#callsign').type('KI5VMF');
          cy.get('#password').type(password);
          cy.get('#confirmPassword').type(password);
          cy.contains('button', 'Create User').click();
          cy.task('hubWait', { name: 'a', pattern: 'Server ready to accept traffic' }, { timeout: 120000 });
        });
      });
      cy.task<string>('workdir', 'onair-b').then((dirB) => {
        cy.task<{ wizard: boolean }>('hubStart', { name: 'b', dir: dirB, env: { ...deployment, ...backends, HTTP_PORT: '3105', DMR_MMDVM_PORT: '62241' } }, { timeout: 120000 })
          .its('wizard').should('eq', false);
      });
    });

    cy.request({ method: 'POST', url: `${hub}/api/v1/auth/login`, body: { username: 'admin', password } }).its('status').should('eq', 200);
    cy.visit(`${hub}/settings`);
    cy.get('[data-test="on-air-card"]').should('contain.text', 'Nobody else can use your ID.');
    cy.get('[data-test="on-air-setup"]').click();
    cy.get('[data-test="on-air-qr"]').should('have.attr', 'src').and('match', /^data:image\/png;base64,/);
    cy.get('[data-test="on-air-key"]').invoke('text').then((shown) => {
      const key = shown.replace(/\s/g, '');
      expect(key).to.match(/^[A-Z2-7]{32}$/);

      cy.get('#on-air-code').type('000000');
      code(key, 0).then((now) => {
        if (now === '000000') {
          return;
        }
        cy.get('[data-test="on-air-turn-on"]').click();
        cy.get('[data-test="on-air-error"]').should('contain.text', 'Wrong code');
      });
      code(key, 0).then((now) => {
        cy.get('#on-air-code').clear().type(now);
        cy.get('[data-test="on-air-turn-on"]').click();
        cy.get('[data-test="on-air-note"]').should('have.text', 'The on-air code is on.');
        cy.get('[data-test="on-air-on"]').should('contain.text', 'private call to 9 and the 6-digit code');
        cy.get('[data-test="on-air-none"]').should('have.text', 'No radio is unlocked now.');

        cy.request(`${replica}/api/v1/users/me/totp`).its('body.on').should('eq', true);
        cy.request({ method: 'DELETE', url: `${replica}/api/v1/users/me/totp`, body: { code: now }, failOnStatusCode: false })
          .its('status').should('eq', 400);
      });

      code(key, 1).then((next) => {
        cy.request({ method: 'DELETE', url: `${replica}/api/v1/users/me/totp`, body: { code: next } }).its('body.on').should('eq', false);
        cy.visit(`${hub}/settings`);
        cy.get('[data-test="on-air-setup"]').should('be.visible');
        cy.get('[data-test="on-air-card"]').should('not.contain.text', 'To unlock');
      });
    });
  });
});
