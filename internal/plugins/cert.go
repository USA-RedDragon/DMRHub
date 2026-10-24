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

// Package plugins serves the plugins: programs that connect over gRPC to
// answer the calls to the DMR IDs an admin gives them.
package plugins

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"strings"
	"sync"
	"time"
)

// CertSource gives the plugin listener its TLS certificate: the certificate
// and key files an admin mounts, read again when they change, or one of its
// own. Its own is an Ed25519 certificate derived from the secret with fixed
// dates, so every replica makes the same certificate and plugins can pin
// its fingerprint.
type CertSource struct {
	certFile string
	keyFile  string
	own      *tls.Certificate

	mu       sync.Mutex
	loaded   *tls.Certificate
	certTime time.Time
	keyTime  time.Time
}

// NewCertSource returns the certificate of a listener with the certificate
// and key files, or of its own from secret when the files are empty.
func NewCertSource(secret []byte, certFile, keyFile string) (*CertSource, error) {
	s := &CertSource{certFile: certFile, keyFile: keyFile}
	if certFile != "" {
		if _, err := s.reload(); err != nil {
			return nil, err
		}
		return s, nil
	}
	own, err := ownCertificate(secret)
	if err != nil {
		return nil, err
	}
	s.own = own
	return s, nil
}

// GetCertificate returns the certificate to present now, for
// tls.Config.GetCertificate.
func (s *CertSource) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	if s.own != nil {
		return s.own, nil
	}
	return s.reload()
}

// Fingerprint returns the SHA-256 of the certificate in use now, as upper
// case hex pairs joined by colons.
func (s *CertSource) Fingerprint() (string, error) {
	cert, err := s.GetCertificate(nil)
	if err != nil {
		return "", err
	}
	return FingerprintOf(cert.Certificate[0]), nil
}

// FingerprintOf returns the SHA-256 of the DER certificate der, as upper
// case hex pairs joined by colons.
func FingerprintOf(der []byte) string {
	sum := sha256.Sum256(der)
	pairs := make([]string, len(sum))
	for i, b := range sum {
		pairs[i] = strings.ToUpper(hex.EncodeToString([]byte{b}))
	}
	return strings.Join(pairs, ":")
}

func (s *CertSource) reload() (*tls.Certificate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ci, err := os.Stat(s.certFile)
	if err != nil {
		return s.keep(fmt.Errorf("reading the plugin certificate file: %w", err))
	}
	ki, err := os.Stat(s.keyFile)
	if err != nil {
		return s.keep(fmt.Errorf("reading the plugin key file: %w", err))
	}
	if s.loaded != nil && ci.ModTime().Equal(s.certTime) && ki.ModTime().Equal(s.keyTime) {
		return s.loaded, nil
	}
	cert, err := tls.LoadX509KeyPair(s.certFile, s.keyFile)
	if err != nil {
		return s.keep(fmt.Errorf("loading the plugin certificate and key files: %w", err))
	}
	s.loaded, s.certTime, s.keyTime = &cert, ci.ModTime(), ki.ModTime()
	return s.loaded, nil
}

func (s *CertSource) keep(err error) (*tls.Certificate, error) {
	if s.loaded != nil {
		return s.loaded, nil
	}
	return nil, err
}

// ownNotBefore and ownNotAfter are the fixed dates of the own certificate,
// so that every replica makes the same one.
var (
	ownNotBefore = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // fixed dates
	ownNotAfter  = time.Date(2126, time.January, 1, 0, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // fixed dates
)

func ownCertificate(secret []byte) (*tls.Certificate, error) {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("DMRHub plugin listener key"))
	key := ed25519.NewKeyFromSeed(mac.Sum(nil))
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "DMRHub plugins"},
		NotBefore:             ownNotBefore,
		NotAfter:              ownNotAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(nil, tmpl, tmpl, key.Public(), key)
	if err != nil {
		return nil, fmt.Errorf("making the plugin certificate: %w", err)
	}
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}
