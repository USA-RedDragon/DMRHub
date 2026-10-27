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

// Package wtcert gives the WebTransport listener its TLS certificate: the
// certificate and key files that an admin mounts, which it reads again when
// they change, or certificates of its own. Its own are short-lived ECDSA
// certificates that every replica derives alike from the secret of the
// config, so that browsers can accept them by hash, which needs no
// certificate authority and no setup.
package wtcert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"os"
	"sync"
	"time"
)

// Path is the path of the WebTransport endpoint of the MMDVM server.
const Path = "/mmdvm"

const (
	// Window is how long one of the own certificates is the one in use.
	Window = 10 * 24 * time.Hour
	// Validity is how long one of the own certificates is valid, counted
	// from the start of its window. Browsers accept a certificate by hash
	// only when it is valid for at most 14 days.
	Validity = 13 * 24 * time.Hour
	// lead is how long before its window a certificate becomes valid, for
	// clocks that run behind.
	lead = time.Hour
)

var errNoKey = errors.New("no valid key")

// Source gives the TLS certificate of the listener.
type Source struct {
	secret   []byte
	certFile string
	keyFile  string

	mu       sync.Mutex
	own      map[int64]owned
	loaded   *tls.Certificate
	certTime time.Time
	keyTime  time.Time
	now      func() time.Time
}

type owned struct {
	cert *tls.Certificate
	hash [sha256.Size]byte
}

// New returns the Source of a listener with the certificate and key files,
// or with certificates of its own derived from secret when both are empty.
// It reads the files once, so that a listener with files that are not a
// key pair does not start.
func New(secret []byte, certFile, keyFile string) (*Source, error) {
	s := &Source{secret: append([]byte(nil), secret...), certFile: certFile, keyFile: keyFile, own: map[int64]owned{}, now: time.Now}
	if s.Files() {
		if _, err := s.reload(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

var (
	sharedMu sync.Mutex //nolint:gochecknoglobals // guards shared
	shared   *Source    //nolint:gochecknoglobals // the Source that Shared returns
)

// Shared returns the Source for secret and the certificate and key files,
// as New does, but returns the same Source again while they stay the same,
// so that the listener and the public route that publishes its hashes make
// each certificate of their own once.
func Shared(secret []byte, certFile, keyFile string) (*Source, error) {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if s := shared; s != nil && hmac.Equal(s.secret, secret) && s.certFile == certFile && s.keyFile == keyFile {
		return s, nil
	}
	s, err := New(secret, certFile, keyFile)
	if err != nil {
		return nil, err
	}
	shared = s
	return s, nil
}

// Files reports whether the Source uses certificate and key files.
func (s *Source) Files() bool {
	return s.certFile != "" || s.keyFile != ""
}

// GetCertificate returns the certificate to present now, for
// tls.Config.GetCertificate.
func (s *Source) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	if s.Files() {
		return s.reload()
	}
	o, err := s.ownFor(window(s.now()))
	if err != nil {
		return nil, err
	}
	return o.cert, nil
}

// Hashes returns the SHA-256 hashes of the own certificate in use now and
// of the next one, for the serverCertificateHashes of a browser, and none
// for a Source with files.
func (s *Source) Hashes() ([][sha256.Size]byte, error) {
	if s.Files() {
		return nil, nil
	}
	w := window(s.now())
	var hashes [][sha256.Size]byte
	for _, k := range []int64{w, w + 1} {
		o, err := s.ownFor(k)
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, o.hash)
	}
	return hashes, nil
}

func window(t time.Time) int64 {
	return t.Unix() / int64(Window/time.Second)
}

func (s *Source) reload() (*tls.Certificate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ci, err := os.Stat(s.certFile)
	if err != nil {
		return s.keep(fmt.Errorf("reading the certificate file: %w", err))
	}
	ki, err := os.Stat(s.keyFile)
	if err != nil {
		return s.keep(fmt.Errorf("reading the key file: %w", err))
	}
	if s.loaded != nil && ci.ModTime().Equal(s.certTime) && ki.ModTime().Equal(s.keyTime) {
		return s.loaded, nil
	}
	cert, err := tls.LoadX509KeyPair(s.certFile, s.keyFile)
	if err != nil {
		return s.keep(fmt.Errorf("loading the certificate and key files: %w", err))
	}
	s.loaded, s.certTime, s.keyTime = &cert, ci.ModTime(), ki.ModTime()
	return s.loaded, nil
}

func (s *Source) keep(err error) (*tls.Certificate, error) {
	if s.loaded != nil {
		return s.loaded, nil
	}
	return nil, err
}

func (s *Source) ownFor(k int64) (owned, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o, ok := s.own[k]; ok {
		return o, nil
	}
	o, err := derive(s.secret, k)
	if err != nil {
		return owned{}, err
	}
	for old := range s.own {
		if old < k-1 {
			delete(s.own, old)
		}
	}
	s.own[k] = o
	return o, nil
}

func derive(secret []byte, k int64) (owned, error) {
	key, err := deriveKey(secret, k)
	if err != nil {
		return owned{}, err
	}
	start := time.Unix(k*int64(Window/time.Second), 0).UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          new(big.Int).SetInt64(k + 1),
		Subject:               pkix.Name{CommonName: "DMRHub WebTransport"},
		NotBefore:             start.Add(-lead),
		NotAfter:              start.Add(Validity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(nil, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return owned{}, fmt.Errorf("making the WebTransport certificate: %w", err)
	}
	return owned{
		cert: &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key},
		hash: sha256.Sum256(der),
	}, nil
}

func deriveKey(secret []byte, k int64) (*ecdsa.PrivateKey, error) {
	if k < 0 {
		return nil, errNoKey
	}
	var msg [16]byte
	binary.BigEndian.PutUint64(msg[:8], uint64(k))
	for try := range uint64(16) {
		binary.BigEndian.PutUint64(msg[8:], try)
		mac := hmac.New(sha256.New, secret)
		mac.Write([]byte("DMRHub WebTransport certificate key"))
		mac.Write(msg[:])
		if key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), mac.Sum(nil)); err == nil {
			return key, nil
		}
	}
	return nil, errNoKey
}
