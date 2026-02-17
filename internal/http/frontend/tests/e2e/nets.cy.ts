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

const netsFixture = {
  total: 2,
  nets: [
    {
      id: 1,
      talkgroup_id: 1,
      talkgroup: { id: 1, name: 'World', description: 'Worldwide talkgroup' },
      started_by_user: { id: 1234567, callsign: 'W5TST' },
      start_time: '2025-01-01T00:00:00Z',
      description: 'Weekly World Net',
      active: true,
      showcase: true,
      check_in_count: 5,
    },
    {
      id: 2,
      talkgroup_id: 2,
      talkgroup: { id: 2, name: 'Local', description: 'Local talkgroup' },
      started_by_user: { id: 1234567, callsign: 'W5TST' },
      start_time: '2025-01-02T00:00:00Z',
      end_time: '2025-01-02T01:00:00Z',
      duration_minutes: 60,
      description: 'Local Evening Net',
      active: false,
      showcase: false,
      check_in_count: 12,
    },
  ],
};

const scheduledNetsFixture = {
  total: 1,
  scheduled_nets: [
    {
      id: 1,
      talkgroup_id: 1,
      talkgroup: { id: 1, name: 'World', description: 'Worldwide talkgroup' },
      created_by_user: { id: 1234567, callsign: 'W5TST' },
      name: 'Weekly World Net',
      description: 'Every Wednesday',
      cron_expression: '0 0 19 * * 3',
      day_of_week: 3,
      time_of_day: '19:00',
      timezone: 'America/Chicago',
      duration_minutes: 60,
      enabled: true,
      next_run: '2025-02-05T19:00:00Z',
      created_at: '2024-12-01T00:00:00Z',
    },
  ],
};

const emptyNets = { total: 0, nets: [] };

describe('Nets Pages', () => {
  beforeEach(() => {
    cy.stubCommonEndpoints();
  });

  describe('Nets list page (/nets)', () => {
    it('displays nets list', () => {
      cy.login();
      cy.intercept(
        {
          method: 'GET',
          pathname: '/api/v1/nets',
          query: { page: /\d+/, limit: /\d+/ },
        },
        {
          statusCode: 200,
          body: netsFixture,
        },
      ).as('getNets');
      cy.intercept(
        { method: 'GET', pathname: '/api/v1/nets', query: { showcase: 'true' } },
        {
          statusCode: 200,
          body: { total: 1, nets: [netsFixture.nets[0]] },
        },
      ).as('getShowcaseNets');


      cy.visit('/nets');
      cy.wait(['@getNets', '@getShowcaseNets']);

      cy.contains('Weekly World Net').should('be.visible');
      cy.contains('Local Evening Net').should('be.visible');
    });

    it('shows showcase nets section with featured nets', () => {
      cy.login();
      cy.intercept(
        {
          method: 'GET',
          pathname: '/api/v1/nets',
          query: { page: /\d+/, limit: /\d+/ },
        },
        {
          statusCode: 200,
          body: netsFixture,
        },
      ).as('getNets');
      cy.intercept(
        { method: 'GET', pathname: '/api/v1/nets', query: { showcase: 'true' } },
        {
          statusCode: 200,
          body: { total: 1, nets: [netsFixture.nets[0]] },
        },
      ).as('getShowcaseNets');

      cy.visit('/nets');
      cy.wait(['@getShowcaseNets']);

      cy.contains('Featured Nets').should('be.visible');
    });

    it('shows empty state when no nets exist', () => {
      cy.login();
      cy.intercept(
        {
          method: 'GET',
          pathname: '/api/v1/nets',
          query: { page: /\d+/, limit: /\d+/ },
        },
        {
          statusCode: 200,
          body: emptyNets,
        },
      ).as('getNetsEmpty');
      cy.intercept(
        { method: 'GET', pathname: '/api/v1/nets', query: { showcase: 'true' } },
        {
          statusCode: 200,
          body: emptyNets,
        },
      ).as('getShowcaseEmpty');
      cy.visit('/nets');
      cy.wait(['@getNetsEmpty', '@getShowcaseEmpty']);

      cy.contains('No nets found').should('be.visible');
    });
  });

  describe('Net details page (/nets/:id)', () => {
    it('displays net details', () => {
      cy.login();
      const net = netsFixture.nets[0];
      cy.intercept('GET', '/api/v1/nets/1', {
        statusCode: 200,
        body: net,
      }).as('getNet');
      cy.intercept('GET', '/api/v1/nets/1/checkins?*', {
        statusCode: 200,
        body: { total: 0, check_ins: [] },
      }).as('getCheckIns');

      cy.visit('/nets/1');
      cy.wait('@getNet');

      cy.contains('Weekly World Net').should('be.visible');
      cy.contains('TG 1').should('be.visible');
    });

    it('shows check-in list', () => {
      cy.login();
      const net = netsFixture.nets[0];
      cy.intercept('GET', '/api/v1/nets/1', {
        statusCode: 200,
        body: net,
      });
      cy.intercept('GET', '/api/v1/nets/1/checkins?*', {
        statusCode: 200,
        body: {
          total: 1,
          check_ins: [
            {
              call_id: 100,
              user: { id: 1234567, callsign: 'W5TST' },
              start_time: '2025-01-01T00:05:00Z',
              duration: 30,
              time_slot: false,
              loss: 0,
              jitter: 0.1,
              ber: 0,
              rssi: -80,
            },
          ],
        },
      }).as('getCheckIns');

      cy.visit('/nets/1');
      cy.wait('@getCheckIns');

      cy.contains('W5TST').should('be.visible');
    });
  });

  describe('Admin nets page (/admin/nets)', () => {
    it('displays admin nets table', () => {
      cy.loginAsAdmin();
      cy.intercept('GET', '/api/v1/nets*', {
        statusCode: 200,
        body: netsFixture,
      }).as('getNets');
      cy.intercept('GET', '/api/v1/nets/scheduled*', {
        statusCode: 200,
        body: scheduledNetsFixture,
      }).as('getScheduledNets');

      cy.visit('/admin/nets');
      cy.wait('@getNets');

      cy.get('h1').contains('Nets').should('be.visible');
      cy.contains('Weekly World Net').should('be.visible');
    });

    it('shows scheduled nets section', () => {
      cy.loginAsAdmin();
      cy.intercept('GET', '/api/v1/nets*', {
        statusCode: 200,
        body: netsFixture,
      }).as('getNets');
      cy.intercept('GET', '/api/v1/nets/scheduled*', {
        statusCode: 200,
        body: scheduledNetsFixture,
      }).as('getScheduledNets');

      cy.visit('/admin/nets');
      cy.wait('@getScheduledNets');

      cy.contains('Scheduled Nets').should('be.visible');
      cy.contains('Weekly World Net').should('be.visible');
    });
  });

  describe('Navigation', () => {
    it('Nets tab is visible in header for unauthenticated users', () => {
      cy.intercept('GET', '/api/v1/users/me', { statusCode: 401, body: {} });

      cy.visit('/');

      cy.get('header').within(() => {
        cy.contains('Nets').should('be.visible');
      });
    });

    it('Admin dropdown includes Nets link for admins', () => {
      cy.loginAsAdmin();
      cy.visit('/');

      cy.get('header').within(() => {
        cy.contains('Admin').click();
      });

      cy.contains('Nets').should('be.visible');
    });
  });
});
