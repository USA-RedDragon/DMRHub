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
  id: 300999901,
  callsign: 'N0CALL',
  latitude: 30.2669,
  longitude: -97.7729,
  height: 12,
  location: 'Zilker Park',
  description: 'Test repeater',
  connected_time: new Date(Date.now() - 2 * 3600000).toISOString(),
  last_ping_time: new Date().toISOString(),
};

function stubPositions(repeaters: (typeof repeater & { talkgroups?: number[] })[]) {
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
    cy.get('[data-test="repeater-300999901"]')
      .should('contain.text', 'N0CALL · 300999901')
      .and('contain.text', '30.2669, −97.7729')
      .and('contain.text', 'Zilker Park · Test repeater')
      .and('contain.text', '2 hours ago');
    cy.get('[data-test="repeater-300999901"] a').should('have.attr', 'href', '/repeaters/300999901');
    cy.get('[data-test="repeater-300999901"]').click();
    cy.get('[data-test="repeater-300999901"]').should('have.class', 'bg-muted/50');
  });

  it('shows no repeaters when none reports a position', () => {
    stubPositions([]);
    cy.visit('/positions');
    cy.wait(['@getPositions', '@getRepeaters']);

    cy.get('[data-test="no-radios"]').should('be.visible');
    cy.get('[data-test="repeaters"]').should('not.exist');
  });
});

type FakeSocket = { url: string; emit(frame: unknown): void };

function fakeSockets(win: Cypress.AUTWindow & { fakeSockets?: FakeSocket[] }) {
  const all: FakeSocket[] = [];
  win.fakeSockets = all;
  class Fake {
    url: string;
    listeners: Record<string, ((event: unknown) => void)[]> = {};
    constructor(url: string) {
      this.url = url;
      all.push(this);
      setTimeout(() => this.fire('open', {}), 0);
    }
    addEventListener(type: string, fn: (event: unknown) => void) {
      (this.listeners[type] ??= []).push(fn);
    }
    fire(type: string, event: unknown) {
      for (const fn of this.listeners[type] ?? []) {
        fn(event);
      }
    }
    emit(frame: unknown) {
      this.fire('message', { data: JSON.stringify(frame) });
    }
    send() {}
    close() {}
  }
  Object.defineProperty(win, 'WebSocket', { value: Fake });
}

function emit(path: string, frame: unknown) {
  cy.window().then((win) => {
    const socket = (win as unknown as { fakeSockets: FakeSocket[] }).fakeSockets.find((s) => s.url.endsWith(path));
    expect(socket, path).to.exist;
    socket!.emit(frame);
  });
}

describe('Positions blips', () => {
  const radio = 3009999;
  const position = {
    radio_id: radio,
    shared: false,
    latitude: 30.2665,
    longitude: -97.7712,
    format: 'lrrp',
    accuracy_m: 10,
    speed_mps: null,
    heading_deg: null,
    altitude_m: null,
    fix_time: null,
    received_at: new Date().toISOString(),
    repeater_id: repeater.id,
    slot: 2,
    service_id: 5057,
  };
  const call = { id: 41, active: true, kind: 'voice', source_id: radio, repeater_id: repeater.id };
  const far = { ...repeater, id: 300999903, callsign: 'N3CALL', latitude: 30.2500, longitude: -97.7475, location: 'South Congress', talkgroups: [3100] };

  beforeEach(() => {
    cy.stubCommonEndpoints();
    cy.login();
    stubPositions([{ ...repeater, talkgroups: [3100] }, far]);
    cy.intercept('GET', '/api/v1/positions/radios', {
      statusCode: 200,
      body: {
        radios: [{ radio_id: radio, tracking: true, interval_minutes: 10, retention_hours: 24, aprs: false, aprs_ssid: '', aprs_symbol: '', aprs_comment: '' }],
        intervals: [5, 10, 15, 30, 60],
        retention_hours: [24, 168, 720],
        aprs: false,
        callsign: 'N0CALL',
      },
    });
    cy.intercept({ method: 'GET', pathname: '/api/v1/positions' }, { statusCode: 200, body: { positions: [position] } }).as('getPositions');
    cy.intercept({ method: 'GET', pathname: `/api/v1/positions/${radio}` }, { statusCode: 200, body: { latest: position, track: [position] } });
    cy.visit('/positions', { onBeforeLoad: fakeSockets });
    cy.wait(['@getPositions', '@getRepeaters']);
    cy.get(`[data-test="radio-${radio}"]`).should('exist');
  });

  it('blips the radio and its repeater while the call is on the air', () => {
    emit('/ws/calls', call);
    cy.get(`[data-test="radio-blip-${radio}"]`).should('have.class', 'tx-blip--loop');
    cy.get(`[data-test="repeater-blip-${repeater.id}"]`).should('have.class', 'tx-blip--loop');
    cy.get(`[data-test="radio-blip-${radio}"] .tx-blip__ring`).should('have.length', 3);

    emit('/ws/lastheard', { ...call, active: false });
    cy.get(`[data-test="radio-blip-${radio}"]`).should('not.exist');
    cy.get(`[data-test="repeater-blip-${repeater.id}"]`).should('not.exist');
  });

  it('gives a data burst one blip that clears by itself', () => {
    emit('/ws/calls', { ...call, id: 42, kind: 'data', active: false });
    cy.get(`[data-test="radio-blip-${radio}"]`).should('have.class', 'tx-blip--burst');
    cy.get(`[data-test="radio-blip-${radio}"]`, { timeout: 3000 }).should('not.exist');
  });

  it('ignores a call from a radio that is not on the map', () => {
    emit('/ws/calls', { ...call, id: 43, source_id: 3008888, repeater_id: 300888801 });
    emit('/ws/calls', { ...call, id: 44 });
    cy.get(`[data-test="radio-blip-${radio}"]`).should('exist');
    cy.get('[data-test^="radio-blip-"]').should('have.length', 1);
    cy.get('[data-test^="repeater-blip-"]').should('have.length', 1);
  });

  it('draws lines on the map between the ends of a call', () => {
    cy.get('[data-test="tx-lines"]', { timeout: 30000 }).should('have.attr', 'data-ready', 'true');
    const tg = { ...call, id: 45, group_call: true, is_to_talkgroup: true, to_talkgroup: { id: 3100 }, destination_id: 3100 };
    emit('/ws/lastheard', tg);
    cy.get(`[data-test="line-45:repeater:${far.id}"]`).should('have.class', 'tx-line--voice');
    cy.get(`[data-test="blip-radio-${radio}"] .tx-blip`).should('have.class', 'tx-blip--loop');
    cy.get(`[data-test="blip-repeater-${repeater.id}"] .tx-blip`).should('have.class', 'tx-blip--loop');
    emit('/ws/lastheard', { ...tg, active: false });
    cy.get(`[data-test="line-45:repeater:${far.id}"]`).should('not.exist');
    cy.get(`[data-test="blip-radio-${radio}"]`).should('not.exist');

    emit('/ws/calls', { ...call, id: 46, kind: 'data', sub_kind: 'sms', active: false, is_to_repeater: true, to_repeater: { id: far.id }, destination_id: far.id });
    cy.get(`[data-test="line-46:repeater:${far.id}"]`).should('have.class', 'tx-line--data');
    cy.get(`[data-test="repeater-blip-${far.id}"]`).should('have.class', 'tx-blip--burst');
    cy.get(`[data-test="line-46:repeater:${far.id}"]`, { timeout: 4000 }).should('not.exist');
  });
});
