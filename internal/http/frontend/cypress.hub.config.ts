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

import { defineConfig } from 'cypress';
import { spawn, spawnSync, execFileSync, type ChildProcess } from 'node:child_process';
import fs from 'node:fs';
import http from 'node:http';
import path from 'node:path';
import process from 'node:process';

type Hub = { proc: ChildProcess; log: string[]; exited: boolean };

const hubs = new Map<string, Hub>();
const containers: string[] = [];
let placeholder: http.Server | null = null;

const scratch = () => {
  const dir = process.env.HUB_SCRATCH;
  if (!dir) {
    throw new Error('set HUB_SCRATCH');
  }
  return dir;
};
const fakeBrowser = () => {
  const dir = path.join(scratch(), 'nobrowser');
  fs.mkdirSync(dir, { recursive: true });
  for (const name of ['xdg-open', 'x-www-browser', 'www-browser', 'sensible-browser']) {
    fs.writeFileSync(path.join(dir, name), '#!/bin/sh\nexit 0\n', { mode: 0o755 });
  }
  return dir;
};

const waitFor = (hub: Hub, pattern: RegExp, from: number, timeout: number): Promise<string> => new Promise((resolve, reject) => {
  const started = Date.now();
  const tick = () => {
    const line = hub.log.slice(from).find((l) => pattern.test(l));
    if (line) {
      resolve(line);
      return;
    }
    if (hub.exited) {
      reject(new Error(`DMRHub exited:\n${hub.log.slice(-30).join('\n')}`));
      return;
    }
    if (Date.now() - started > timeout) {
      reject(new Error(`no ${pattern} within ${timeout} ms:\n${hub.log.slice(-30).join('\n')}`));
      return;
    }
    setTimeout(tick, 100);
  };
  tick();
});

const stop = (name: string): Promise<null> => new Promise((resolve) => {
  const hub = hubs.get(name);
  if (!hub || hub.exited) {
    resolve(null);
    return;
  }
  hub.proc.once('exit', () => resolve(null));
  hub.proc.kill('SIGTERM');
  setTimeout(() => hub.proc.kill('SIGKILL'), 20000);
});

export default defineConfig({
  video: false,
  screenshotOnRunFailure: true,
  defaultCommandTimeout: 15000,
  e2e: {
    specPattern: 'tests/hub/**/*.cy.ts',
    supportFile: false,
    setupNodeEvents(on) {
      on('task', {
        placeholderStart() {
          if (placeholder) {
            return null;
          }
          return new Promise((resolve) => {
            placeholder = http.createServer((_, res) => res.end('<html><body>DMRHub tests</body></html>'));
            placeholder.listen(3005, () => resolve(null));
          });
        },
        placeholderStop() {
          return new Promise((resolve) => {
            if (!placeholder) {
              resolve(null);
              return;
            }
            placeholder.close(() => resolve(null));
            placeholder.closeAllConnections();
            placeholder = null;
          });
        },
        workdir(name: string) {
          const dir = path.join(scratch(), 'hubs', `${name}-${Date.now()}`);
          fs.mkdirSync(path.join(dir, 'home'), { recursive: true });
          return dir;
        },
        mkdir(dir: string) {
          fs.mkdirSync(dir, { recursive: true });
          return null;
        },
        fileMode(file: string) {
          return fs.existsSync(file) ? fs.statSync(file).mode & 0o777 : null;
        },
        readFile(file: string) {
          return fs.existsSync(file) ? fs.readFileSync(file, 'utf8') : null;
        },
        async hubStart({ name, dir, env }: { name: string; dir: string; env: Record<string, string> }) {
          await stop(name);
          const bin = process.env.HUB_BIN;
          if (!bin) {
            throw new Error('set HUB_BIN');
          }
          const hub: Hub = { proc: spawn(bin, [], {
            cwd: dir,
            env: {
              PATH: `${fakeBrowser()}:${process.env.PATH ?? ''}`,
              HOME: path.join(dir, 'home'),
              XDG_CONFIG_HOME: path.join(dir, 'home', '.config'),
              GOMAXPROCS: '2',
              DMR_RADIO_ID_URL: 'http://127.0.0.1:9/users.json',
              DMR_REPEATER_ID_URL: 'http://127.0.0.1:9/rptrs.json',
              ...env,
            },
            stdio: ['ignore', 'pipe', 'pipe'],
          }), log: [], exited: false };
          const take = (b: Buffer) => {
            fs.appendFileSync(path.join(dir, 'hub.log'), b);
            for (const l of b.toString().split('\n')) {
              if (l !== '') {
                hub.log.push(l);
              }
            }
          };
          hub.proc.stdout?.on('data', take);
          hub.proc.stderr?.on('data', take);
          hub.proc.on('exit', () => { hub.exited = true; });
          hubs.set(name, hub);
          const line = await waitFor(hub, /Opening setup wizard at|Server ready to accept traffic/, 0, 90000);
          const m = /token=([^\s"]+)/.exec(line);
          return { token: m ? m[1] : null };
        },
        async hubWait({ name, pattern }: { name: string; pattern: string }) {
          const hub = hubs.get(name);
          if (!hub) {
            throw new Error(`no hub ${name}`);
          }
          await waitFor(hub, new RegExp(pattern), 0, 90000);
          return null;
        },
        hubLog(name: string) {
          return hubs.get(name)?.log.join('\n') ?? '';
        },
        hubStop(name: string) {
          return stop(name);
        },
        async hubStopAll() {
          await Promise.all([...hubs.keys()].map(stop));
          return null;
        },
        backends() {
          const run = (args: string[]) => execFileSync('docker', ['run', '-d', '--rm', '--cpus', '2', '-P', ...args]).toString().trim();
          const pg = run(['-e', 'POSTGRES_PASSWORD=hub', '-e', 'POSTGRES_DB=hub', 'postgres:17-alpine']);
          containers.push(pg);
          const redis = run(['redis:7-alpine']);
          containers.push(redis);
          const port = (id: string, p: string) => execFileSync('docker', ['port', id, p]).toString().split('\n')[0]!.split(':').pop()!.trim();
          const pgPort = port(pg, '5432/tcp');
          let ready = 0;
          for (let i = 0; i < 240 && ready < 3; i++) {
            const probe = spawnSync('docker', ['exec', pg, 'psql', '-h', '127.0.0.1', '-U', 'postgres', '-d', 'hub', '-c', 'select 1'], { stdio: 'ignore' });
            ready = probe.status === 0 ? ready + 1 : 0;
            spawnSync('sleep', ['0.5']);
          }
          return {
            DATABASE_DRIVER: 'postgres', DATABASE_HOST: '127.0.0.1', DATABASE_PORT: pgPort,
            DATABASE_USERNAME: 'postgres', DATABASE_PASSWORD: 'hub', DATABASE_DATABASE: 'hub', DATABASE_EXTRA_PARAMETERS: 'sslmode=disable',
            REDIS_ENABLED: 'true', REDIS_HOST: '127.0.0.1', REDIS_PORT: port(redis, '6379/tcp'),
          };
        },
        stopBackends() {
          for (const id of containers.splice(0)) {
            spawnSync('docker', ['rm', '-f', id], { stdio: 'ignore' });
          }
          return null;
        },
      });
      on('after:run', async () => {
        await Promise.all([...hubs.keys()].map(stop));
        for (const id of containers.splice(0)) {
          spawnSync('docker', ['rm', '-f', id], { stdio: 'ignore' });
        }
      });
    },
  },
});
