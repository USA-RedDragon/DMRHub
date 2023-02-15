// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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

// https://docs.cypress.io/api/introduction/api.html

beforeEach(() => {
  cy.intercept("/api/v1/users/me", {
    id: 3191868,
    callsign: "KI5VMF",
    username: "USA-RedDragon",
    admin: true,
    approved: true,
    suspended: false,
    created_at: "2023-01-27T21:50:34.154146-06:00",
  });
  cy.intercept("/api/v1/version", {
    body: "1.1.0",
  });
  cy.intercept(
    "/api/v1/lastheard?page=1&limit=10",
    JSON.stringify({
      total: 1,
      calls: [
        {
          id: 86,
          start_time: "2023-02-13T20:38:36.578332-06:00",
          duration: 540760412,
          active: false,
          user: {
            id: 3191868,
            callsign: "KI5VMF",
            username: "USA-RedDragon",
          },
          time_slot: true,
          group_call: true,
          is_to_talkgroup: true,
          to_talkgroup: {
            id: 1,
            name: "General",
          },
          is_to_user: false,
          to_user: {
            id: 0,
            callsign: "",
          },
          is_to_repeater: false,
          to_repeater: {
            id: 0,
            callsign: "",
          },
          destination_id: 1,
          loss: 0,
          jitter: -0.46484375,
          ber: 0.0011862395,
          rssi: 46.816406,
        },
      ],
    })
  );
});

describe("Screenshotter", () => {
  it("visits the app root url while not signed in", () => {
    cy.intercept("/api/v1/users/me", {
      statusCode: 401,
    });
    cy.visit("/");
    cy.screenshot({
      onBeforeScreenshot: () => {
        cy.get("#app").waitForStableDOM({ pollInterval: 1000, timeout: 10000 });
      },
    });
  });
  it("visits the app root url while signed in", () => {
    cy.visit("/");
    cy.screenshot({
      onBeforeScreenshot: () => {
        cy.get("#app").waitForStableDOM({ pollInterval: 1000, timeout: 10000 });
      },
    });
  });
  it("clicks the theme picker", () => {
    cy.visit("/");
    cy.get(".pi-palette").click();
    cy.get(".layout-config-sidebar", { timeout: 6000 }).should("be.visible");
    cy.screenshot({
      onBeforeScreenshot: () => {
        cy.get("#app").waitForStableDOM({ pollInterval: 1000, timeout: 10000 });
      },
    });
  });
  it("clicks the theme picker and makes the size smaller", () => {
    cy.visit("/");
    cy.get(".pi-palette").click();
    cy.get(".layout-config-sidebar", { timeout: 6000 }).should("be.visible");
    cy.get(".p-sidebar-content div .p-button .pi-minus").click();
    cy.get(".p-sidebar-close-icon").click();
    cy.get(".layout-config-sidebar", { timeout: 6000 }).should("not.exist");
    cy.screenshot({
      onBeforeScreenshot: () => {
        cy.get("#app").waitForStableDOM({ pollInterval: 1000, timeout: 10000 });
      },
    });
  });
  it("clicks the theme picker and makes the size even smaller", () => {
    cy.visit("/");
    cy.get(".pi-palette").click();
    cy.get(".layout-config-sidebar", { timeout: 6000 }).should("be.visible");
    cy.get(".p-sidebar-content div .p-button .pi-minus").click();
    cy.get(".p-sidebar-content div .p-button .pi-minus").click();
    cy.get(".p-sidebar-close-icon").click();
    cy.get(".layout-config-sidebar", { timeout: 6000 }).should("not.exist");
    cy.screenshot({
      onBeforeScreenshot: () => {
        cy.get("#app").waitForStableDOM({ pollInterval: 1000, timeout: 10000 });
      },
    });
  });
  it("clicks the theme picker and makes the size the smallest", () => {
    cy.visit("/");
    cy.get(".pi-palette").click();
    cy.get(".layout-config-sidebar", { timeout: 6000 }).should("be.visible");
    cy.get(".p-sidebar-content div .p-button .pi-minus").click();
    cy.get(".p-sidebar-content div .p-button .pi-minus").click();
    cy.get(".p-sidebar-content div .p-button .pi-minus").click();
    cy.get(".p-sidebar-close-icon").click();
    cy.get(".layout-config-sidebar", { timeout: 6000 }).should("not.exist");
    cy.screenshot({
      onBeforeScreenshot: () => {
        cy.get("#app").waitForStableDOM({ pollInterval: 1000, timeout: 10000 });
      },
    });
  });
  it("clicks the theme picker and makes the size bigger", () => {
    cy.visit("/");
    cy.get(".pi-palette").click();
    cy.get(".layout-config-sidebar", { timeout: 6000 }).should("be.visible");
    cy.get(".p-sidebar-content div .p-button .pi-plus").click();
    cy.get(".p-sidebar-close-icon").click();
    cy.get(".layout-config-sidebar", { timeout: 6000 }).should("not.exist");
    cy.screenshot({
      onBeforeScreenshot: () => {
        cy.get("#app").waitForStableDOM({ pollInterval: 1000, timeout: 10000 });
      },
    });
  });
  it("clicks the theme picker and makes the size even bigger", () => {
    cy.visit("/");
    cy.get(".pi-palette").click();
    cy.get(".layout-config-sidebar", { timeout: 6000 }).should("be.visible");
    cy.get(".p-sidebar-content div .p-button .pi-plus").click();
    cy.get(".p-sidebar-content div .p-button .pi-plus").click();
    cy.get(".p-sidebar-close-icon").click();
    cy.get(".layout-config-sidebar", { timeout: 6000 }).should("not.exist");
    cy.screenshot({
      onBeforeScreenshot: () => {
        cy.get("#app").waitForStableDOM({ pollInterval: 1000, timeout: 10000 });
      },
    });
  });
  it("clicks the theme picker and makes the size the biggest", () => {
    cy.visit("/");
    cy.get(".pi-palette").click();
    cy.get(".layout-config-sidebar", { timeout: 6000 }).should("be.visible");
    cy.get(".p-sidebar-content div .p-button .pi-plus").click();
    cy.get(".p-sidebar-content div .p-button .pi-plus").click();
    cy.get(".p-sidebar-content div .p-button .pi-plus").click();
    cy.get(".p-sidebar-close-icon").click();
    cy.get(".layout-config-sidebar", { timeout: 6000 }).should("not.exist");
    cy.screenshot({
      onBeforeScreenshot: () => {
        cy.get("#app").waitForStableDOM({ pollInterval: 1000, timeout: 10000 });
      },
    });
  });
  it("is different themes", () => {
    const themes = [
      "arya-blue",
      "arya-green",
      "arya-orange",
      "arya-purple",
      "bootstrap4-dark-blue",
      "bootstrap4-dark-purple",
      "bootstrap4-light-blue",
      "bootstrap4-light-purple",
      "fluent-light",
      "lara-dark-blue",
      "lara-dark-indigo",
      "lara-dark-purple",
      "lara-dark-teal",
      "lara-light-blue",
      "lara-light-indigo",
      "lara-light-purple",
      "lara-light-teal",
      "luna-amber",
      "luna-blue",
      "luna-green",
      "luna-pink",
      "mdc-dark-deeppurple",
      "mdc-dark-indigo",
      "mdc-light-deeppurple",
      "mdc-light-indigo",
      "md-dark-deeppurple",
      "md-dark-indigo",
      "md-light-deeppurple",
      "md-light-indigo",
      "nova",
      "nova-accent",
      "nova-alt",
      "nova-vue",
      "rhea",
      "saga-blue",
      "saga-green",
      "saga-orange",
      "saga-purple",
      "tailwind-light",
      "vela-blue",
      "vela-green",
      "vela-orange",
      "vela-purple",
    ];
    themes.forEach((theme) => {
      cy.visit("/", {
        onBeforeLoad: function (window) {
          window.localStorage.setItem("theme", theme);
        },
      });
      cy.screenshot(theme, {
        onBeforeScreenshot: () => {
          cy.get("#app").waitForStableDOM({
            pollInterval: 1000,
            timeout: 10000,
          });
        },
      });
    });
  });
});
