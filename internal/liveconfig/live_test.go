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

package liveconfig_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/liveconfig"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	cpflag "github.com/USA-RedDragon/configulator/v2/flags/pflag"
	"github.com/goccy/go-yaml"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func backends() []testutils.Backend {
	return []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()}
}

func newDB(t *testing.T, be testutils.Backend) *gorm.DB {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = fmt.Sprintf("file:memdb_live_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	be.Setup(t, &cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return database
}

// deployment is a replica's config file, environment and flags.
type deployment struct {
	file  string
	env   map[string]string
	flags []string
}

// load loads the config of d as DMRHub does, with its file found at path.
func load(t *testing.T, path string, d deployment) (*config.Config, *configulator.Report) {
	t.Helper()
	if d.file != "" {
		require.NoError(t, os.WriteFile(path, []byte(d.file), 0o600))
	}
	c := configulator.New(config.ConfigSchema()).
		WithEnvironmentVariables(&configulator.EnvironmentVariableOptions{Separator: "_"}).
		WithEnviron(func(k string) (string, bool) { v, ok := d.env[k]; return v, ok }).
		WithFile(&configulator.FileOptions{
			Search:   []string{path},
			Decoders: configulator.Decoders{".yaml": yaml.Unmarshal},
		})
	fs := pflag.NewFlagSet("DMRHub", pflag.ContinueOnError)
	cpflag.Bind(c, fs, config.ConfigPFlagHooks(), nil)
	require.NoError(t, fs.Parse(d.flags))
	cfg, err := c.LoadWithoutValidation()
	require.NoError(t, err)
	return cfg, c.Report()
}

// boot starts a replica of d on database as DMRHub does and returns its
// Live.
func boot(t *testing.T, database *gorm.DB, d deployment) *liveconfig.Live {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	base, report := load(t, path, d)
	l := liveconfig.New(liveconfig.Options{Base: base, Locks: liveconfig.LocksFrom(report), DB: database, LoadedFile: report.File().Path})
	require.NoError(t, l.Boot(context.Background()))
	return l
}

const validDeployment = "secret: a-secret-that-is-long-enough-for-the-check\npassword-salt: a-salt-that-is-long-enough-for-the-check\nhttp:\n  canonical-host: http://localhost\n"

func save(t *testing.T, l *liveconfig.Live, change func(*config.Config)) *liveconfig.SaveResult {
	t.Helper()
	next := *l.Current().Get()
	change(&next)
	res, errs, err := l.Save(context.Background(), &next)
	require.NoError(t, err)
	require.Empty(t, errs)
	return res
}

// TestEverySettingIsClassified makes a new setting of config.Config fail
// until it is in exactly one of the Setup and start-only lists.
func TestEverySettingIsClassified(t *testing.T) {
	t.Parallel()
	for _, k := range liveconfig.Keys() {
		assert.True(t, liveconfig.Classified(k.Path), "%s must be in exactly one of setupPaths and bootPaths", k.Path)
		if k.Secret {
			assert.False(t, k.Setup, "the secret %s must be start-only", k.Path)
		}
	}
	paths := map[string]bool{}
	for _, k := range liveconfig.Keys() {
		paths[k.Path] = k.Setup
	}
	for p, setup := range map[string]bool{
		"secret": false, "database.host": false, "redis.enabled": false, "http.port": false, "smtp.password": false,
		"dmr.plugins.port": false, "dmr.ipsc.network-id": true, "dmr.transcoding.ambe-servers": true,
		"dmr.plugins.in-process": true, "log-level": true, "smtp.host": true,
	} {
		got, ok := paths[p]
		require.True(t, ok, p)
		assert.Equal(t, setup, got, p)
	}
}

// TestPrecedence runs a replica whose config file, environment and flags
// set some settings that Setup also saved: defaults, then the saved
// settings, then the file, then the environment, then the flags.
func TestPrecedence(t *testing.T) {
	t.Parallel()
	for _, be := range backends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			database := newDB(t, be)
			setup := boot(t, database, deployment{file: validDeployment})
			save(t, setup, func(c *config.Config) {
				c.NetworkName = "Saved"
				c.DMR.IPSC.NetworkID = 111111
				c.DMR.OpenBridge.ServerID = 1111
				c.DMR.Retention.Messages = 11
				c.DMR.Retention.Bursts = 1
			})

			l := boot(t, database, deployment{
				file:  validDeployment + "dmr:\n  ipsc:\n    network-id: 222222\n  openbridge:\n    server-id: 2222\n  retention:\n    messages: 22\n",
				env:   map[string]string{"DMR_OPENBRIDGE_SERVER_ID": "3333", "DMR_RETENTION_MESSAGES": "33"},
				flags: []string{"--dmr.retention.messages=44"},
			})
			got := l.Current().Get()
			assert.Equal(t, "Saved", got.NetworkName, "saved over the default")
			assert.Equal(t, 1, got.DMR.Retention.Bursts, "saved over the default")
			assert.Zero(t, got.DMR.Retention.Transmissions, "the default without a save")
			assert.Equal(t, uint32(222222), got.DMR.IPSC.NetworkID, "the file over the saved value")
			assert.Equal(t, uint32(3333), got.DMR.OpenBridge.ServerID, "the environment over the file")
			assert.Equal(t, 44, got.DMR.Retention.Messages, "a flag over the environment")

			locks := l.Locks()
			assert.Equal(t, liveconfig.SourceFile, locks["dmr.ipsc.network-id"].Source)
			assert.Equal(t, liveconfig.SourceEnv, locks["dmr.openbridge.server-id"].Source)
			assert.Equal(t, liveconfig.SourceFlag, locks["dmr.retention.messages"].Source)
			assert.NotContains(t, locks, "network-name")

			st, err := l.Status(context.Background())
			require.NoError(t, err)
			assert.Equal(t, liveconfig.SourceSetup, st.Settings["network-name"].Source)
			assert.True(t, st.Settings["network-name"].Editable)
			assert.Equal(t, liveconfig.SourceFile, st.Settings["dmr.ipsc.network-id"].Source)
			assert.True(t, st.Settings["dmr.ipsc.network-id"].Locked)
			assert.False(t, st.Settings["dmr.ipsc.network-id"].Editable)
			assert.JSONEq(t, "111111", string(st.Settings["dmr.ipsc.network-id"].Saved), "the saved value stays under the lock")
			assert.False(t, st.Settings["dmr.ipsc.network-id"].NotInEffect)
			assert.True(t, st.Settings["database.host"].StartOnly)
		})
	}
}

// TestPublicAddressIsASetupSetting saves the public address in Setup and
// sets it back to its default, which removes the saved value.
func TestPublicAddressIsASetupSetting(t *testing.T) {
	t.Parallel()
	database := newDB(t, testutils.SQLiteMemoryBackend())
	l := boot(t, database, deployment{file: validDeployment})
	save(t, l, func(c *config.Config) { c.DMR.PublicAddress = "dmr.example.org" })
	st, err := l.Status(context.Background())
	require.NoError(t, err)
	s := st.Settings["dmr.public-address"]
	assert.True(t, s.Editable)
	assert.False(t, s.StartOnly)
	assert.Equal(t, liveconfig.SourceSetup, s.Source)
	assert.JSONEq(t, `"dmr.example.org"`, string(s.Saved))
	assert.JSONEq(t, `""`, string(s.Default))
	assert.Equal(t, "dmr.example.org", l.Current().Get().ServerAddress())

	save(t, l, func(c *config.Config) { c.DMR.PublicAddress = "" })
	st, err = l.Status(context.Background())
	require.NoError(t, err)
	assert.Equal(t, liveconfig.SourceDefault, st.Settings["dmr.public-address"].Source)
	assert.Equal(t, "localhost", l.Current().Get().ServerAddress())
}

// TestALockedListIsLockedWhole sets one AMBE server in the config file.
// The list is locked as a whole, and the saved list does not mix in.
func TestALockedListIsLockedWhole(t *testing.T) {
	t.Parallel()
	database := newDB(t, testutils.SQLiteMemoryBackend())
	setup := boot(t, database, deployment{file: validDeployment})
	save(t, setup, func(c *config.Config) {
		c.DMR.Transcoding.AMBEServers = []config.AMBEServer{{Address: "saved-a:2460"}, {Address: "saved-b:2460", Calls: 1}}
	})
	l := boot(t, database, deployment{file: validDeployment + "dmr:\n  transcoding:\n    ambe-servers:\n      - address: file:2460\n"})
	assert.Equal(t, []config.AMBEServer{{Address: "file:2460"}}, l.Current().Get().DMR.Transcoding.AMBEServers)
	assert.Equal(t, liveconfig.SourceFile, l.Locks()["dmr.transcoding.ambe-servers"].Source)

	next := *l.Current().Get()
	next.DMR.Transcoding.AMBEServers = []config.AMBEServer{{Address: "file:2460"}, {Address: "more:2460"}}
	errs := l.Check(&next)
	require.Len(t, errs, 1)
	assert.Equal(t, "dmr.transcoding.ambe-servers", errs[0].Field)
	assert.ErrorIs(t, errs[0].Err, liveconfig.ErrLocked)
}

// TestRemovingALockBringsBackTheSavedValue boots a replica whose
// environment sets settings, then boots it again without them: the value
// saved in Setup comes back, or the default when none was saved.
func TestRemovingALockBringsBackTheSavedValue(t *testing.T) {
	t.Parallel()
	for _, be := range backends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			database := newDB(t, be)
			setup := boot(t, database, deployment{file: validDeployment})
			save(t, setup, func(c *config.Config) { c.DMR.IPSC.NetworkID = 888888 })

			env := map[string]string{"DMR_IPSC_NETWORK_ID": "777777", "DMR_RETENTION_BURSTS": "9"}
			locked := boot(t, database, deployment{file: validDeployment, env: env})
			assert.Equal(t, uint32(777777), locked.Current().Get().DMR.IPSC.NetworkID)
			assert.Equal(t, 9, locked.Current().Get().DMR.Retention.Bursts)

			next := *locked.Current().Get()
			next.NetworkName = "From the locked replica"
			_, errs, err := locked.Save(context.Background(), &next)
			require.NoError(t, err)
			require.Empty(t, errs)

			free := boot(t, database, deployment{file: validDeployment})
			assert.Equal(t, uint32(888888), free.Current().Get().DMR.IPSC.NetworkID, "the saved value comes back")
			assert.Zero(t, free.Current().Get().DMR.Retention.Bursts, "the default comes back")
			assert.Equal(t, "From the locked replica", free.Current().Get().NetworkName)
		})
	}
}

// TestSaveRefusesWhatSetupDoesNotChange refuses a change of a locked
// setting, a start-only setting and a secret, and saves no part of it.
func TestSaveRefusesWhatSetupDoesNotChange(t *testing.T) {
	t.Parallel()
	database := newDB(t, testutils.SQLiteMemoryBackend())
	l := boot(t, database, deployment{file: validDeployment + "dmr:\n  ipsc:\n    network-id: 222222\n"})
	cases := []struct {
		field  string
		want   error
		change func(*config.Config)
	}{
		{"dmr.ipsc.network-id", liveconfig.ErrLocked, func(c *config.Config) { c.DMR.IPSC.NetworkID = 1 }},
		{"secret", liveconfig.ErrLocked, func(c *config.Config) { c.Secret = "another-secret-that-is-long-enough" }},
		{"http.port", liveconfig.ErrStartOnly, func(c *config.Config) { c.HTTP.Port = 8080 }},
		{"smtp.password", liveconfig.ErrStartOnly, func(c *config.Config) { c.SMTP.Password = "new" }},
		{"redis.enabled", liveconfig.ErrStartOnly, func(c *config.Config) { c.Redis.Enabled = true }},
	}
	for _, tc := range cases {
		next := *l.Current().Get()
		next.NetworkName = "Changed"
		tc.change(&next)
		res, errs, err := l.Save(context.Background(), &next)
		require.NoError(t, err)
		assert.Nil(t, res, tc.field)
		require.Len(t, errs, 1, tc.field)
		assert.Equal(t, tc.field, errs[0].Field)
		assert.True(t, errors.Is(errs[0].Err, tc.want), "%s: %v", tc.field, errs[0].Err)
	}
	saved, err := liveconfig.LoadSaved(context.Background(), database)
	require.NoError(t, err)
	assert.Empty(t, saved.Values, "a refused save stores nothing")
	assert.Equal(t, "DMRHub", l.Current().Get().NetworkName)
}

// TestSaveStoresOnlyChangedSetupSettings saves a config with Setup
// settings changed and the start-only settings as they run. Only the
// changed Setup settings reach the database, never a secret, and a setting
// set back to its default is no longer saved.
func TestSaveStoresOnlyChangedSetupSettings(t *testing.T) {
	t.Parallel()
	database := newDB(t, testutils.SQLiteMemoryBackend())
	l := boot(t, database, deployment{file: validDeployment})
	res := save(t, l, func(c *config.Config) {
		c.NetworkName = "Net"
		c.DMR.Retention.Messages = 30
		c.SMTP.Host = "mail.example.com"
	})
	assert.Equal(t, uint64(1), res.Revision)
	require.Len(t, res.Replicas, 1)
	assert.True(t, res.Replicas[0].Self)
	assert.False(t, res.Failed())

	saved, err := liveconfig.LoadSaved(context.Background(), database)
	require.NoError(t, err)
	assert.Equal(t, map[string]json.RawMessage{
		"network-name": json.RawMessage(`"Net"`), "dmr.retention.messages": json.RawMessage("30"), "smtp.host": json.RawMessage(`"mail.example.com"`),
	}, saved.Values)

	save(t, l, func(c *config.Config) { c.DMR.Retention.Messages = 0 })
	saved, err = liveconfig.LoadSaved(context.Background(), database)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), saved.Revision)
	assert.NotContains(t, saved.Values, "dmr.retention.messages")
	assert.Contains(t, saved.Values, "network-name")
}

// TestBootImportsOnlyTheFileThatDMRHubWroteAndLoads boots the first time
// after the upgrade. It imports the Setup settings of the loaded file only
// when that is the file that DMRHub used to write, and only once.
func TestBootImportsOnlyTheFileThatDMRHubWroteAndLoads(t *testing.T) {
	t.Parallel()
	written := validDeployment + "network-name: Written\nlog-level: info\ndmr:\n  ipsc:\n    network-id: 311860\n"
	legacy := func(t *testing.T) string {
		t.Helper()
		dir := filepath.Join(t.TempDir(), "DMRHub")
		require.NoError(t, os.Mkdir(dir, 0o700))
		return filepath.Join(dir, "config.yaml")
	}

	t.Run("loaded", func(t *testing.T) {
		t.Parallel()
		database := newDB(t, testutils.SQLiteMemoryBackend())
		path := legacy(t)
		base, report := load(t, path, deployment{file: written, env: map[string]string{"NETWORK_NAME": "From the environment"}})
		l := liveconfig.New(liveconfig.Options{Base: base, Locks: liveconfig.LocksFrom(report), DB: database, LoadedFile: report.File().Path, LegacyFile: path})
		require.NoError(t, l.Boot(context.Background()))
		saved, err := liveconfig.LoadSaved(context.Background(), database)
		require.NoError(t, err)
		assert.Equal(t, map[string]json.RawMessage{
			"dmr.ipsc.network-id": json.RawMessage("311860"), "http.canonical-host": json.RawMessage(`"http://localhost"`),
		}, saved.Values, "only Setup settings that differ from the default and that the environment leaves alone")

		require.NoError(t, os.WriteFile(path, []byte(validDeployment), 0o600))
		again := boot(t, database, deployment{file: validDeployment})
		assert.Equal(t, uint32(311860), again.Current().Get().DMR.IPSC.NetworkID, "the imported value outlives its line in the file")

		require.NoError(t, os.WriteFile(path, []byte(validDeployment+"dmr:\n  retention:\n    messages: 5\n"), 0o600))
		base, report = load(t, path, deployment{})
		l = liveconfig.New(liveconfig.Options{Base: base, Locks: liveconfig.LocksFrom(report), DB: database, LoadedFile: report.File().Path, LegacyFile: path})
		require.NoError(t, l.Boot(context.Background()))
		saved, err = liveconfig.LoadSaved(context.Background(), database)
		require.NoError(t, err)
		assert.NotContains(t, saved.Values, "dmr.retention.messages", "the import runs once")
	})

	t.Run("not loaded", func(t *testing.T) {
		t.Parallel()
		database := newDB(t, testutils.SQLiteMemoryBackend())
		legacyPath := legacy(t)
		require.NoError(t, os.WriteFile(legacyPath, []byte(written), 0o600))
		local := filepath.Join(t.TempDir(), "config.yaml")
		base, report := load(t, local, deployment{file: validDeployment})
		l := liveconfig.New(liveconfig.Options{Base: base, Locks: liveconfig.LocksFrom(report), DB: database, LoadedFile: report.File().Path, LegacyFile: legacyPath})
		require.NoError(t, l.Boot(context.Background()))
		saved, err := liveconfig.LoadSaved(context.Background(), database)
		require.NoError(t, err)
		assert.Empty(t, saved.Values, "a written file that the hub did not load holds nothing it ran with")
		assert.Equal(t, "DMRHub", l.Current().Get().NetworkName)
	})
}

// TestBootMakesTheSecretsOnce boots replicas whose deployment sets no
// secret. The first makes a secret and a password salt, every later boot
// and replica gets the same ones, a secret that the environment sets wins,
// and no status shows them.
func TestBootMakesTheSecretsOnce(t *testing.T) {
	t.Parallel()
	for _, be := range backends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			database := newDB(t, be)
			first := boot(t, database, deployment{})
			got := first.Current().Get()
			assert.GreaterOrEqual(t, len(got.Secret), 40)
			assert.GreaterOrEqual(t, len(got.PasswordSalt), 40)
			assert.NotEqual(t, got.Secret, got.PasswordSalt)

			again := boot(t, database, deployment{})
			assert.Equal(t, got.Secret, again.Current().Get().Secret)
			assert.Equal(t, got.PasswordSalt, again.Current().Get().PasswordSalt)

			locked := boot(t, database, deployment{env: map[string]string{"SECRET": "a-secret-that-the-environment-sets"}})
			assert.Equal(t, "a-secret-that-the-environment-sets", locked.Current().Get().Secret)
			assert.Equal(t, got.PasswordSalt, locked.Current().Get().PasswordSalt)

			st, err := again.Status(context.Background())
			require.NoError(t, err)
			assert.Equal(t, liveconfig.SourceGenerated, st.Settings["secret"].Source)
			assert.False(t, st.Settings["secret"].Editable)
			b, err := json.Marshal(st)
			require.NoError(t, err)
			assert.NotContains(t, string(b), got.Secret)
			assert.NotContains(t, string(b), got.PasswordSalt)

			next := *again.Current().Get()
			next.NetworkName = "Net"
			next.HTTP.CanonicalHost = "http://localhost"
			_, errs, err := again.Save(context.Background(), &next)
			require.NoError(t, err)
			assert.Empty(t, errs, "a save keeps the made secrets")
		})
	}
}
