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

const hub = 'http://localhost:3005';
const password = 'Correct-Horse-Battery-9';

type Started = { token: string | null };

const start = (name: string, dir: string, env: Record<string, string> = {}) =>
  cy.task<Started>('hubStart', { name, dir, env }, { timeout: 120000 });

const openWizard = (token: string | null) => {
  expect(token, 'the setup wizard must start').to.be.a('string');
  cy.visit(`${hub}/setup?token=${token}`);
  cy.contains('h1', 'Setup').should('be.visible');
  cy.contains('General Settings').should('be.visible');
};

const save = () => {
  cy.contains('button', 'Save').click();
};

const createAdmin = (name: string) => {
  cy.location('pathname', { timeout: 20000 }).should('eq', '/setup/user');
  cy.get('#dmr_id').type('3191868');
  cy.get('#username').type('admin');
  cy.get('#callsign').type('KI5VMF');
  cy.get('#password').type(password);
  cy.get('#confirmPassword').type(password);
  cy.contains('button', 'Create User').click();
  cy.task('hubWait', { name, pattern: 'Server ready to accept traffic' }, { timeout: 120000 });
};

const login = (base = hub) =>
  cy.request({ method: 'POST', url: `${base}/api/v1/auth/login`, body: { username: 'admin', password } })
    .its('status').should('eq', 200);

const fillSetup = () => {
  cy.get('#http-canonical-host').clear();
  cy.get('#http-canonical-host').type('http://localhost:3005');
  cy.get('label[for="disable-radio-id-validation"]').click();
  save();
};

describe('First start and the setup wizard', () => {
  before(() => {
    cy.task('placeholderStart');
    cy.visit(hub);
    cy.task('placeholderStop');
  });

  afterEach(() => {
    cy.task('hubStopAll', null, { timeout: 60000 });
  });

  after(() => {
    cy.task('stopBackends', null, { timeout: 60000 });
  });

  it('sets up a fresh install with nothing set, and a restart keeps the made secrets', () => {
    cy.task<string>('workdir', 'fresh').then((dir) => {
      start('fresh', dir).then(({ token }) => {
        openWizard(token);
        cy.get('#secret').should('have.attr', 'readonly');
        cy.contains('DMRHub made a random value at its first start and keeps it in the database.').should('be.visible');
        cy.get('#network-name').should('not.have.attr', 'readonly');
        fillSetup();
        createAdmin('fresh');
      });
      login();
      cy.request(`${hub}/api/v1/users/me`).its('body.username').should('eq', 'admin');
      cy.task('readFile', `${dir}/dmrhub-database.json`).should('eq', null);

      cy.task('hubStop', 'fresh', { timeout: 60000 });
      start('fresh', dir).its('token').should('eq', null);
      cy.request(`${hub}/api/v1/users/me`).its('body.username').should('eq', 'admin', 'the session of before the restart still works, so the secret is the same');
      cy.clearCookies();
      login();
      cy.task<string>('hubLog', 'fresh').should('not.match', /Opening setup wizard/);
    });
  });

  it('fixes a database that DMRHub cannot reach, keeps the fix, and the next start uses it', () => {
    cy.task<string>('workdir', 'baddb').then((dir) => {
      cy.task('mkdir', `${dir}/DMRHub.db`);
      start('baddb', dir).then(({ token }) => {
        openWizard(token);
        cy.get('[data-test="database-problem"]').should('contain.text', 'DMRHub cannot reach its database.')
          .and('contain.text', `${dir}/dmrhub-database.json`);
        cy.get('#network-name').should('have.attr', 'readonly');
        cy.get('#database-database').should('not.have.attr', 'readonly');
        cy.get('#database-database').clear();
        cy.get('#database-database').type('hub.db');
        save();
        cy.get('[data-test="database-problem"]', { timeout: 20000 }).should('not.exist');
        cy.get('#network-name').should('not.have.attr', 'readonly');
        fillSetup();
        createAdmin('baddb');
      });
      cy.task('fileMode', `${dir}/dmrhub-database.json`).should('eq', 0o600);
      cy.task<string>('readFile', `${dir}/dmrhub-database.json`).should('contain', 'hub.db');
      login();

      cy.task('hubStop', 'baddb', { timeout: 60000 });
      start('baddb', dir).its('token').should('eq', null);
      cy.clearCookies();
      login();
    });
  });

  it('asks only for the admin when the deployment sets everything', () => {
    const env = {
      SECRET: 'a-secret-that-the-deployment-sets-1234',
      PASSWORD_SALT: 'a-salt-that-the-deployment-sets-5678',
      HTTP_CANONICAL_HOST: 'http://localhost:3005',
      NETWORK_NAME: 'Deployed Net',
      DMR_DISABLE_RADIO_ID_VALIDATION: 'true',
    };
    cy.task<string>('workdir', 'locked').then((dir) => {
      start('locked', dir, env).then(({ token }) => {
        openWizard(token);
        cy.get('#network-name').should('have.value', 'Deployed Net');
        cy.get('#network-name').should('have.attr', 'readonly');
        cy.get('label[for="network-name"]').should('contain.text', 'Set by the deployment');
        cy.get('#http-canonical-host').should('have.attr', 'readonly');
        cy.get('#secret').should('have.attr', 'readonly');
        cy.get('#disable-radio-id-validation').should('be.disabled');
        cy.contains('The environment of the deployment sets this. Change it there and restart DMRHub.').should('be.visible');
        save();
        createAdmin('locked');
      });
      login();
      cy.request(`${hub}/api/v1/config`).its('body').then((body) => {
        expect(body['network-name']).to.eq('Deployed Net');
        expect(body.settings['network-name'].locked).to.eq(true);
        expect(body.settings['network-name'].source).to.eq('env');
      });
    });
  });

  it('shares a save with a second replica that joins, and shows both replicas', () => {
    cy.task<Record<string, string>>('backends', null, { timeout: 180000 }).then((backends) => {
      cy.task<string>('workdir', 'replica-a').then((dirA) => {
        start('a', dirA, { ...backends, DMR_MMDVM_PORT: '62131' }).then(({ token }) => {
          openWizard(token);
          fillSetup();
          createAdmin('a');
        });
      });
      cy.task<string>('workdir', 'replica-b').then((dirB) => {
        start('b', dirB, { ...backends, HTTP_PORT: '3105', DMR_MMDVM_PORT: '62231' }).its('token').should('eq', null);
      });
      login();
      cy.visit(`${hub}/admin/setup`);
      cy.get('[data-test="replicas"]', { timeout: 30000 }).find('tbody tr').should('have.length', 2);
      cy.get('[data-test="replicas-differ"]').should('contain.text', 'Port (MMDVM)');
      cy.get('#network-name').clear();
      cy.get('#network-name').type('Shared Net');
      cy.intercept('PUT', '**/api/v1/config').as('save');
      save();
      cy.wait('@save').then(({ response }) => {
        expect(response?.statusCode).to.eq(200);
        const replicas = response?.body.replicas as { self: boolean; answered: boolean }[];
        expect(replicas).to.have.length(2);
        expect(replicas.every((r) => r.answered), 'every replica reports before the save returns').to.eq(true);
      });
      cy.get('[data-test="apply-errors"]').should('not.exist');
      cy.request('http://localhost:3105/api/v1/network/name').its('body').should('eq', 'Shared Net');
    });
  });
});
