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

package plugins_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheOwnCertificateIsTheSameOnEveryReplica(t *testing.T) {
	t.Parallel()
	secret := []byte("a secret of the config, 15+ chars")
	a, err := plugins.NewCertSource(secret, "", "")
	require.NoError(t, err)
	b, err := plugins.NewCertSource(secret, "", "")
	require.NoError(t, err)
	fa, err := a.Fingerprint()
	require.NoError(t, err)
	fb, err := b.Fingerprint()
	require.NoError(t, err)
	assert.Equal(t, fa, fb)
	assert.Len(t, fa, 32*3-1)

	other, err := plugins.NewCertSource([]byte("another secret of 15+ chars"), "", "")
	require.NoError(t, err)
	fo, err := other.Fingerprint()
	require.NoError(t, err)
	assert.NotEqual(t, fa, fo)

	cert, err := a.GetCertificate(nil)
	require.NoError(t, err)
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err)
	assert.Equal(t, x509.Ed25519, parsed.PublicKeyAlgorithm)
	assert.Equal(t, 2126, parsed.NotAfter.Year())
}

func TestTheCertificateFilesAreReadAgainWhenTheyChange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	_, err := plugins.NewCertSource(nil, certFile, keyFile)
	require.Error(t, err, "missing files")

	write := func(seed string) {
		t.Helper()
		src, err := plugins.NewCertSource([]byte(seed), "", "")
		require.NoError(t, err)
		cert, err := src.GetCertificate(nil)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(certFile, pemOf("CERTIFICATE", cert.Certificate[0]), 0o600))
		key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(keyFile, pemOf("PRIVATE KEY", key), 0o600))
	}
	write("first secret, long enough")
	src, err := plugins.NewCertSource(nil, certFile, keyFile)
	require.NoError(t, err)
	first, err := src.Fingerprint()
	require.NoError(t, err)

	write("second secret, long enough")
	mtimeLater(t, certFile, keyFile)
	second, err := src.Fingerprint()
	require.NoError(t, err)
	assert.NotEqual(t, first, second)

	require.NoError(t, os.WriteFile(certFile, []byte("broken"), 0o600))
	mtimeLater(t, certFile, keyFile)
	kept, err := src.Fingerprint()
	require.NoError(t, err, "a broken file keeps the certificate in use")
	assert.Equal(t, second, kept)
	_, err = tls.X509KeyPair([]byte("broken"), nil)
	require.Error(t, err)
}

func pemOf(kind string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der})
}

var mtimeStep int64 //nolint:gochecknoglobals // a test clock for file times

func mtimeLater(t *testing.T, files ...string) {
	t.Helper()
	at := time.Now().Add(time.Duration(atomic.AddInt64(&mtimeStep, 1)) * time.Minute)
	for _, f := range files {
		require.NoError(t, os.Chtimes(f, at, at))
	}
}
