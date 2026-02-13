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

import { addMilliseconds, addSeconds, differenceInMilliseconds, max, parseISO, sub } from 'date-fns';

// https://docs.cypress.io/api/introduction/api.html

// DMR radio IDs lifted randomly from radioid.net
const radioIds = [
  {
    id: 3110691,
    callsign: 'KF6FM',
  },
  {
    id: 2353426,
    callsign: 'MW6ABC',
  },
  {
    id: 3163099,
    callsign: 'KO4CVD',
  },
  {
    id: 2626282,
    callsign: 'DK4FC',
  },
];

function generateUser(lastUser) {
  const radioId = radioIds[Math.floor(Math.random() * radioIds.length)];

  if (lastUser && lastUser.id === radioId.id) {
    return generateUser(lastUser);
  }

  return {
    id: radioId.id,
    callsign: radioId.callsign,
  };
}

function generateCall(id, callTime, user) {
  const dst = Math.floor(Math.random() * 2) + 1;
  const slot = Math.floor(Math.random() * 2) === 0;
  return {
    id,
    active: false,
    time_slot: slot,
    group_call: true,
    start_time: callTime.start,
    duration: callTime.duration,
    user,
    is_to_talkgroup: true,
    to_talkgroup: {
      id: dst,
    },
    destination_id: dst,
    loss: Math.random() * 0.032,
    jitter: Math.random() * 6 - 3,
    ber: Math.random() * 0.1,
    rssi: Math.random() * 9 + 32,
  };
}

// Generates an array of calls to be used in the lastheard API
function generateCalls(count) {
  const calls = [];
  let lastStart;
  let lastDuration = 2 * 1000 * 1000 * 1000;
  let lastUser = generateUser(null);

  if (count > 30) {
    lastStart = sub(new Date(), { hours: 3 }).toISOString();

    for (let i = 0; i < count - 30; i++) {
      const callTime = generateCallTime(lastStart, lastDuration);
      const user = generateUser(lastUser);

      calls.push(generateCall(i, callTime, user));
      lastStart = callTime.start;
      lastDuration = callTime.duration;
      lastUser = user;
    }

    lastStart = sub(new Date(), { minutes: 10 }).toISOString();

    for (let i = count + 0; i < count + 30; i++) {
      const callTime = generateCallTime(lastStart, lastDuration);
      const user = generateUser(lastUser);

      calls.push(generateCall(i, callTime, user));
      lastStart = callTime.start;
      lastDuration = callTime.duration;
      lastUser = user;
    }
  } else {
    lastStart = sub(new Date(), { hours: 3 }).toISOString();

    for (let i = 0; i < count; i++) {
      const callTime = generateCallTime(lastStart, lastDuration);
      const user = generateUser(lastUser);

      calls.push(generateCall(i, callTime, user));
      lastStart = callTime.start;
      lastDuration = callTime.duration;
      lastUser = user;
    }
  }

  // Reverse the array so the calls are in order
  return calls.reverse();
}

// Generate call time generates a random time since lastStart + lastDuration
// It returns an object with start and duration
// The start time should not be closer than 3 seconds to lastStart + lastDuration
// Calls should roughly be 3 seconds to 2 minutes long but weighted towards minimum
function generateCallTime(lastStart, lastDuration) {
  const lastStartObj = parseISO(lastStart);

  // Convert lastDuration from nanoseconds to seconds
  const lastDurationSeconds = Math.floor(lastDuration / (1000 * 1000 * 1000));

  // Calculate the minimum start time as lastStart + lastDuration + 3 seconds
  const minStartTimeMoment = addSeconds(lastStartObj, lastDurationSeconds + 3);

  // Generate a random start time between minStartTime and now
  const maxStartTimeMoment = new Date();
  const rangeMs = Math.max(1, differenceInMilliseconds(maxStartTimeMoment, minStartTimeMoment) + 1);
  let startMoment = addMilliseconds(minStartTimeMoment, Math.floor(Math.random() * rangeMs));

  // Ensure the start time is at least 3 seconds after lastStart + lastDuration
  const earliestStartMoment = addSeconds(lastStartObj, lastDurationSeconds + 3);
  startMoment = max([startMoment, earliestStartMoment]);

  const start = startMoment.toISOString();

  const minDuration = 1.2; // minimum duration in seconds
  const maxDuration = 120; // maximum duration in seconds
  const lambda = 0.042; // rate parameter for the exponential distribution
  const randomDuration = -Math.log(1 - Math.random()) / lambda;
  const duration =
    Math.max(minDuration, Math.min(maxDuration, randomDuration)) *
    1000 *
    1000 *
    1000;

  return { start, duration };
}

beforeEach(() => {
  cy.intercept('/api/v1/users/me', {
    id: 3191868,
    callsign: 'KI5VMF',
    username: 'USA-RedDragon',
    admin: true,
    approved: true,
    suspended: false,
    created_at: '2023-01-27T21:50:34.154146-06:00',
  });
  cy.intercept('/api/v1/version', {
    body: '1.1.0',
  });
  cy.intercept(
    '/api/v1/lastheard?page=1&limit=30',
    JSON.stringify({
      total: 50,
      calls: generateCalls(50),
    }),
  );
});

describe('Screenshotter', () => {
  it('visits the app root url while not signed in', () => {
    cy.intercept('/api/v1/users/me', {
      statusCode: 401,
    });
    cy.visit('/');
    cy.get('body').should('be.visible');
    cy.screenshot();
  });
  it('visits the app root url while signed in', () => {
    cy.visit('/');
    cy.get('body').should('be.visible');
    cy.screenshot();
  });
});
