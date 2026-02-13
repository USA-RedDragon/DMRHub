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

type MessageHandler = (event: MessageEvent) => void;

const initialReconnectDelay = 300;
const maxReconnectDelay = 15000;

class WebsocketConnection {
  url: string;
  timeoutTimer: ReturnType<typeof setTimeout> | null;
  timeout: number;
  socket: WebSocket | null;
  onMessage: MessageHandler;
  currentReconnectDelay: number;
  shouldReconnect: boolean;
  reconnectTimer: ReturnType<typeof setTimeout> | null;

  constructor(url: string, onMessage: MessageHandler) {
    this.url = url;
    this.timeoutTimer = null;
    this.timeout = 3000;
    this.socket = null;
    this.onMessage = onMessage;
    this.currentReconnectDelay = initialReconnectDelay;
    this.shouldReconnect = true;
    this.reconnectTimer = null;
  }

  connect() {
    if (!this.shouldReconnect) {
      return;
    }
    this.socket = new WebSocket(this.url);
    this.mapSocketEvents();
  }

  onWebsocketOpen() {
    console.log('Connected to websocket');
    this.currentReconnectDelay = initialReconnectDelay;
    this.socket?.send('PING');
  }

  onWebsocketError() {
    console.log('Disconnected from websocket');
    this.socket = null;
    this.reconnectToWebsocket();
  }

  onWebsocketClose() {
    console.log('Websocket connection closed');
    this.socket = null;
    this.reconnectToWebsocket();
  }

  close() {
    this.shouldReconnect = false;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.socket) {
      this.socket.close();
    }
  }

  reconnectToWebsocket() {
    if (!this.shouldReconnect || this.reconnectTimer) {
      return;
    }

    this.reconnectTimer = setTimeout(() => {
      if (this.currentReconnectDelay < maxReconnectDelay) {
        this.currentReconnectDelay *= 2;
      }
      this.reconnectTimer = null;
      this.connect();
    }, this.currentReconnectDelay + Math.floor(Math.random() * 1000));
  }

  mapSocketEvents() {
    if (!this.socket) {
      return;
    }

    this.socket.addEventListener('open', this.onWebsocketOpen.bind(this));
    this.socket.addEventListener('error', this.onWebsocketError.bind(this));
    this.socket.addEventListener('close', this.onWebsocketClose.bind(this));

    this.socket.addEventListener('message', (event) => {
      if (event.data === 'PONG') {
        setTimeout(() => {
          this.socket?.send('PING');
        }, 1000);
        return;
      }

      this.onMessage(event);
    });
  }
}

export default {
  connect(url: string, onMessage: MessageHandler) {
    const ws = new WebsocketConnection(url, onMessage);
    ws.connect();
    return {
      close() {
        ws.close();
      },
    };
  },
};
