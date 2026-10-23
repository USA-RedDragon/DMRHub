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

package wtcert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func at(t *testing.T, secret string, now time.Time) *Source {
	t.Helper()
	s, err := New([]byte(secret), "", "")
	require.NoError(t, err)
	s.now = func() time.Time { return now }
	return s
}

func TestOwnCertificatesAreTheSameOnEveryReplica(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	one, err := at(t, "secret", now).Hashes()
	require.NoError(t, err)
	two, err := at(t, "secret", now).Hashes()
	require.NoError(t, err)
	assert.Equal(t, one, two)
	other, err := at(t, "another secret", now).Hashes()
	require.NoError(t, err)
	assert.NotEqual(t, one, other)
}

func TestOwnCertificateIsValidAndHashedForBrowsers(t *testing.T) {
	t.Parallel()
	for _, now := range []time.Time{
		time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC),
		time.Unix(window(time.Now())*int64(Window/time.Second), 0),
		time.Unix((window(time.Now())+1)*int64(Window/time.Second)-1, 0),
	} {
		s := at(t, "secret", now)
		cert, err := s.GetCertificate(nil)
		require.NoError(t, err)
		leaf, err := x509.ParseCertificate(cert.Certificate[0])
		require.NoError(t, err)
		assert.True(t, now.After(leaf.NotBefore) && now.Before(leaf.NotAfter), "valid at %v", now)
		assert.LessOrEqual(t, leaf.NotAfter.Sub(leaf.NotBefore), 14*24*time.Hour, "browsers take a hash only for 14 days")
		_, ok := leaf.PublicKey.(*ecdsa.PublicKey)
		assert.True(t, ok, "browsers take a hash only of an ECDSA certificate")
		hashes, err := s.Hashes()
		require.NoError(t, err)
		require.Len(t, hashes, 2)
		assert.Equal(t, sha256.Sum256(cert.Certificate[0]), hashes[0])
		next := at(t, "secret", now.Add(Window))
		nextCert, err := next.GetCertificate(nil)
		require.NoError(t, err)
		assert.Equal(t, sha256.Sum256(nextCert.Certificate[0]), hashes[1], "the next certificate is published before it is used")
	}
}

func writePair(t *testing.T, dir, cn string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tls.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tls.key"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600))
}

func commonName(t *testing.T, s *Source) string {
	t.Helper()
	cert, err := s.GetCertificate(nil)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err)
	return leaf.Subject.CommonName
}

func TestFilesAreReadAgainWhenTheyChange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writePair(t, dir, "first")
	s, err := New(nil, filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key"))
	require.NoError(t, err)
	assert.Equal(t, "first", commonName(t, s))
	hashes, err := s.Hashes()
	require.NoError(t, err)
	assert.Empty(t, hashes, "a certificate from files is not taken by hash")

	writePair(t, dir, "renewed")
	later := time.Now().Add(time.Minute)
	require.NoError(t, os.Chtimes(filepath.Join(dir, "tls.crt"), later, later))
	require.NoError(t, os.Chtimes(filepath.Join(dir, "tls.key"), later, later))
	assert.Equal(t, "renewed", commonName(t, s))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "tls.key"), []byte("broken"), 0o600))
	assert.Equal(t, "renewed", commonName(t, s), "a broken renewal keeps the certificate in use")
}

func TestFilesThatAreNotAPairDoNotStart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := New(nil, filepath.Join(dir, "missing.crt"), filepath.Join(dir, "missing.key"))
	assert.Error(t, err)
}
