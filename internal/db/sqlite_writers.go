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

package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"gorm.io/gorm"
)

// sqliteWriters is a pool of SQLite connections whose transactions all
// begin immediately, so each takes the one write lock of the database.
// SQLite makes the other writers poll for that lock with longer and longer
// sleeps, so a writer that begins again as soon as it commits keeps
// winning it from one that has waited a while, until the busy timeout of
// that one runs out. sqliteWriters gives the lock to the writers of this
// process in the order they ask for it instead.
type sqliteWriters struct {
	*sql.DB
	turn chan struct{}
}

func newSQLiteWriters(pool *sql.DB) *sqliteWriters {
	return &sqliteWriters{DB: pool, turn: make(chan struct{}, 1)}
}

func (p *sqliteWriters) GetDBConn() (*sql.DB, error) {
	return p.DB, nil
}

func (p *sqliteWriters) wait(ctx context.Context) error {
	select {
	case p.turn <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("could not wait for a turn to write: %w", ctx.Err())
	}
}

func (p *sqliteWriters) done() {
	<-p.turn
}

func (p *sqliteWriters) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	if err := p.wait(ctx); err != nil {
		return nil, err
	}
	tx, err := p.DB.BeginTx(ctx, opts)
	if err != nil {
		p.done()
		return nil, fmt.Errorf("could not begin a transaction: %w", err)
	}
	return &sqliteWrite{Tx: tx, pool: p.DB, done: sync.OnceFunc(p.done)}, nil
}

func (p *sqliteWriters) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if err := p.wait(ctx); err != nil {
		return nil, err
	}
	defer p.done()
	return p.DB.ExecContext(ctx, query, args...) //nolint:wrapcheck
}

// sqliteWrite is a transaction that holds the turn of its sqliteWriters
// until it ends.
type sqliteWrite struct {
	*sql.Tx
	pool *sql.DB
	done func()
}

func (t *sqliteWrite) GetDBConn() (*sql.DB, error) {
	return t.pool, nil
}

func (t *sqliteWrite) Commit() error {
	defer t.done()
	return t.Tx.Commit() //nolint:wrapcheck
}

func (t *sqliteWrite) Rollback() error {
	defer t.done()
	return t.Tx.Rollback() //nolint:wrapcheck
}
