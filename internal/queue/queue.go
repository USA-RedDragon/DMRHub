// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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

package queue

// Queue is a simple in-memory queue implementation.
// It uses a map to store multiple byte slices under a single key.
type Queue struct {
	data map[string][][]byte // Key -> Array of byte slices
}

func NewQueue() *Queue {
	return &Queue{
		data: make(map[string][][]byte),
	}
}

func (q *Queue) Push(key string, value []byte) (int, error) {
	q.data[key] = append(q.data[key], value)
	return len(q.data[key]), nil
}

func (q *Queue) Drain(key string) [][]byte {
	values := q.data[key]
	delete(q.data, key)
	return values
}

func (q *Queue) Delete(key string) error {
	delete(q.data, key)
	return nil
}
