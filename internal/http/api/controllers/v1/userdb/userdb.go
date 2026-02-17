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

package userdb

import (
	"net/http"
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/userdb"
	"github.com/biter777/countries"
	"github.com/gin-gonic/gin"
)

type UserDBResponse struct {
	userdb.DMRUser
	Flag string `json:"flag"`
}

// alpha2ToFlag converts a 2-letter country code (e.g. "US") into a flag emoji
// by mapping each ASCII letter to its Regional Indicator Symbol counterpart.
func alpha2ToFlag(alpha2 string) string {
	if len(alpha2) != 2 {
		return ""
	}
	const regionalA = '\U0001F1E6' // Regional Indicator Symbol Letter A
	r0 := regionalA + rune(alpha2[0]) - 'A'
	r1 := regionalA + rune(alpha2[1]) - 'A'
	return string([]rune{r0, r1})
}

func GETUserDBEntry(c *gin.Context) {
	id := c.Param("id")

	userID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid User ID"})
		return
	}

	// Parrot user (9990) is a synthetic user not in the RadioID database.
	if uint(userID) == dmrconst.ParrotUser {
		c.JSON(http.StatusOK, UserDBResponse{
			DMRUser: userdb.DMRUser{
				ID:       dmrconst.ParrotUser,
				Callsign: "Parrot",
				FName:    "Parrot",
				Name:     "Parrot",
				RadioID:  dmrconst.ParrotUser,
			},
			Flag: "\U0001F99C", // 🦜
		})
		return
	}

	dmrUser, ok := userdb.Get(uint(userID))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found in RadioID database"})
		return
	}

	alpha2 := countries.ByName(dmrUser.Country).Alpha2()
	flag := alpha2ToFlag(alpha2)

	resp := UserDBResponse{
		DMRUser: dmrUser,
		Flag:    flag,
	}

	c.JSON(http.StatusOK, resp)
}
