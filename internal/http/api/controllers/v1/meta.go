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

package v1

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/wtcert"
	"github.com/gin-gonic/gin"
)

const (
	errorKey = "error"
)

func GETNetworkName(c *gin.Context) {
	config, ok := utils.GetConfig(c)
	if !ok {
		return
	}

	_, err := io.WriteString(c.Writer, config.NetworkName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error getting network name"})
	}
}

// GETNetworkWebTransport tells browser hotspots and radios where the
// WebTransport listener of the MMDVM server is: its UDP port and path, and,
// when it serves certificates of its own, their SHA-256 hashes, base64
// encoded, for the serverCertificateHashes of the WebTransport API.
func GETNetworkWebTransport(c *gin.Context) {
	config, ok := utils.GetConfig(c)
	if !ok {
		return
	}
	wt := config.DMR.MMDVM.WebTransport
	if !wt.Enabled {
		c.JSON(http.StatusOK, gin.H{"enabled": false})
		return
	}
	certs, err := wtcert.New(config.GetDerivedSecret(), wt.CertFile, wt.KeyFile)
	var hashes [][sha256.Size]byte
	if err == nil {
		hashes, err = certs.Hashes()
	}
	if err != nil {
		slog.Error("Error getting the WebTransport certificate hashes", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error getting the WebTransport certificate"})
		return
	}
	type certificateHash struct {
		Algorithm string `json:"algorithm"`
		Value     string `json:"value"`
	}
	list := make([]certificateHash, 0, len(hashes))
	for _, h := range hashes {
		list = append(list, certificateHash{Algorithm: "sha-256", Value: base64.StdEncoding.EncodeToString(h[:])})
	}
	c.JSON(http.StatusOK, gin.H{
		"enabled":            true,
		"port":               wt.Port,
		"path":               wtcert.Path,
		"certificate_hashes": list,
	})
}

// GETNetworkFeatures answers which optional servers are on, for the
// frontend to show their pages without the admin-only config.
func GETNetworkFeatures(c *gin.Context) {
	config, ok := utils.GetConfig(c)
	if !ok {
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ipsc":       config.DMR.IPSC.Enabled,
		"openbridge": config.DMR.OpenBridge.Enabled,
	})
}

func GETVersion(c *gin.Context) {
	version, ok := c.MustGet("Version").(string)
	if !ok {
		slog.Error("Unable to get Version from context", "function", "GETVersion")
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Try again later"})
		return
	}

	commit, ok := c.MustGet("Commit").(string)
	if !ok {
		slog.Error("Unable to get Commit from context", "function", "GETVersion")
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Try again later"})
		return
	}

	_, err := io.WriteString(c.Writer, fmt.Sprintf("%s-%s", version, commit))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error getting version"})
	}
}

func GETPing(c *gin.Context) {
	_, err := io.WriteString(c.Writer, fmt.Sprintf("%d", time.Now().Unix()))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Error getting ping"})
	}
}

func GETHealthcheck(c *gin.Context) {
	if !utils.GetReady(c) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "healthy"})
}
