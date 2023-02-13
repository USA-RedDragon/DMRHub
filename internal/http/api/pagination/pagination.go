// SPDX-License-Identifier: AGPL-3.0-only
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

package pagination

import "gorm.io/gorm"

type Paginate struct {
	limit int
	page  int
}

func NewPaginate(limit int, page int) *Paginate {
	return &Paginate{limit: limit, page: page}
}

func (p *Paginate) Paginate(db *gorm.DB) *gorm.DB {
	offset := (p.page - 1) * p.limit

	return db.Offset(offset).Limit(p.limit)
}
