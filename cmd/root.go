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

package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/netscheduler"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/manager"
	"github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/liveconfig"
	"github.com/USA-RedDragon/DMRHub/internal/messages"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/plugins/inside"
	"github.com/USA-RedDragon/DMRHub/internal/plugins/wasm"
	"github.com/USA-RedDragon/DMRHub/internal/positions"
	"github.com/USA-RedDragon/DMRHub/internal/pprof"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/repeaterdb"
	"github.com/USA-RedDragon/DMRHub/internal/retention"
	"github.com/USA-RedDragon/DMRHub/internal/userdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-co-op/gocron/v2"
	"github.com/lmittmann/tint"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"gorm.io/gorm"
)

func NewCommand(version, commit string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "DMRHub",
		Version: fmt.Sprintf("%s - %s", version, commit),
		Annotations: map[string]string{
			"version": version,
			"commit":  commit,
		},
		RunE:              runRoot,
		SilenceErrors:     true,
		DisableAutoGenTag: true,
	}
	return cmd
}

func runRoot(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	version, commit := cmd.Annotations["version"], cmd.Annotations["commit"]
	fmt.Printf("DMRHub - %s (%s)\n", version, commit)

	c, base, err := loadConfig(ctx)
	if err != nil {
		return err
	}

	setupLogger(base)

	booted, exit, err := bootConfig(ctx, c.Report(), base, version, commit)
	if err != nil || exit {
		return err
	}
	live, database, current := booted.live, booted.db, booted.live.Current()
	cfg := current.Get()

	setupLogger(cfg)

	scheduler, err := setupScheduler()
	if err != nil {
		return err
	}

	setupDMRDatabaseJobs(current, scheduler)

	scheduler.Start()

	cleanup, err := setupTracing(cfg)
	if err != nil {
		return fmt.Errorf("failed to setup tracing: %w", err)
	}
	defer func() {
		if err := cleanup(ctx); err != nil {
			slog.Error("Failed to shutdown tracer", "error", err)
		}
	}()

	startBackgroundServices(cfg)

	kvStore, err := kv.MakeKV(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to key-value store: %w", err)
	}

	pubsubClient, err := pubsub.MakePubSub(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to pubsub: %w", err)
	}

	callTracker := calltracker.NewCallTracker(database, pubsubClient, kvStore)
	messageStore := messages.NewStore(database, kvStore)
	messageStore.SetNotify(messages.NewPusher(database, pubsubClient).Push)
	callTracker.SetDataObserver(messageStore)

	dmrHub := hub.NewHub(database, kvStore, pubsubClient, callTracker)

	servers, err := initializeServers(ctx, live, dmrHub, callTracker, kvStore, pubsubClient, database, version, commit)
	if err != nil {
		return err
	}
	servers.messageStore = messageStore
	defer servers.shutdown(ctx)

	setupShutdownHandlers(ctx, scheduler, dmrHub, servers, cleanup)

	return nil
}

type booted struct {
	live *liveconfig.Live
	db   *gorm.DB
}

// bootConfig connects to the database and reads the Setup settings. It
// runs the setup wizard while DMRHub cannot reach its database, and while
// the config is not valid or no admin exists. exit is true when a signal
// ends the wizard.
func bootConfig(ctx context.Context, report *configulator.Report, base *config.Config, version, commit string) (booted, bool, error) {
	locks := liveconfig.LocksFrom(report)
	state := liveconfig.DatabaseStateIn()
	wizardDB := false
	if !liveconfig.DatabaseLocked(locks) {
		d, ok, err := state.Load()
		switch {
		case err != nil:
			slog.Error("Failed to read the database setting of the setup wizard", "file", state.Path, "error", err)
		case ok:
			withState := *base
			withState.Database = d
			base, wizardDB = &withState, true
		}
	}

	var wiz *wizard
	defer func() {
		if wiz != nil {
			wiz.stop(ctx)
		}
	}()
	var fixed *liveconfig.Fixed
	database, err := db.MakeDB(base) //nolint:contextcheck // MakeDB takes no context; it connects and migrates once at start
	if err != nil {
		slog.Error("DMRHub cannot reach its database; the setup wizard can fix the database setting", "error", err)
		if wiz, err = startWizard(base, version, commit); err != nil {
			return booted{}, false, err
		}
		f, exit := wiz.waitForDatabase(liveconfig.NewDatabaseFix(base, locks, state, db.MakeDB, err))
		if exit {
			return booted{}, true, nil
		}
		fixed, database, base, wizardDB = &f, f.DB, f.Config, true
	}

	live := liveconfig.New(liveconfig.Options{
		Base:           base,
		Locks:          locks,
		DB:             database,
		LoadedFile:     report.File().Path,
		LegacyFile:     legacyConfigFile(),
		WizardDatabase: wizardDB,
	})
	if err := live.Boot(ctx); err != nil {
		return booted{}, false, fmt.Errorf("failed to read the Setup settings: %w", err)
	}
	needSetup, err := needsSetup(live.Current().Get(), database)
	if err != nil {
		return booted{}, false, err
	}
	if fixed != nil {
		phase := liveconfig.PhaseDone
		if needSetup {
			phase = liveconfig.PhaseSetup
		}
		fixed.Done(phase)
	}
	if needSetup {
		if wiz == nil {
			if wiz, err = startWizard(base, version, commit); err != nil {
				return booted{}, false, err
			}
		}
		if wiz.waitForSetup(live, database) {
			return booted{}, true, nil
		}
	}
	return booted{live: live, db: database}, false, nil
}

// needsSetup reports whether the setup wizard must run: the config is not
// valid, or no admin exists.
func needsSetup(cfg *config.Config, database *gorm.DB) (bool, error) {
	if err := cfg.Validate(); err != nil {
		slog.Info("Configuration validation failed", "error", err)
		return true, nil
	}
	admins, err := models.CountUserAdmins(database)
	if err != nil {
		return false, fmt.Errorf("failed to count the admins: %w", err)
	}
	if admins == 0 {
		slog.Info("No admin exists yet")
	}
	return admins == 0, nil
}

// legacyConfigFile is the file that DMRHub wrote the settings of the Setup
// page to before it kept them in the database.
func legacyConfigFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "DMRHub", "config.yaml")
}

// loadConfig loads the configuration from context, without the saved
// Setup settings.
func loadConfig(ctx context.Context) (*configulator.Configulator[config.Config], *config.Config, error) {
	c, err := configulator.FromContext[config.Config](ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get config from context: %w", err)
	}

	cfg, err := c.LoadWithoutValidation()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	return c, cfg, nil
}

// setupLogger configures the structured logger and the gin mode
func setupLogger(cfg *config.Config) {
	var logger *slog.Logger
	switch cfg.LogLevel {
	case config.LogLevelDebug:
		logger = slog.New(tint.NewTextHandler(os.Stdout, &tint.Options{Level: slog.LevelDebug}))
	case config.LogLevelInfo:
		logger = slog.New(tint.NewTextHandler(os.Stdout, &tint.Options{Level: slog.LevelInfo}))
	case config.LogLevelWarn:
		logger = slog.New(tint.NewTextHandler(os.Stderr, &tint.Options{Level: slog.LevelWarn}))
	case config.LogLevelError:
		logger = slog.New(tint.NewTextHandler(os.Stderr, &tint.Options{Level: slog.LevelError}))
	default:
		// Fall back to info level for unrecognized log levels to prevent nil logger panic
		logger = slog.New(tint.NewTextHandler(os.Stdout, &tint.Options{Level: slog.LevelInfo}))
	}
	slog.SetDefault(logger)

	if cfg.LogLevel == config.LogLevelDebug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
}

// setupScheduler creates and configures the job scheduler
func setupScheduler() (gocron.Scheduler, error) {
	scheduler, err := gocron.NewScheduler()
	if err != nil {
		return nil, fmt.Errorf("failed to create scheduler: %w", err)
	}
	return scheduler, nil
}

// setupTracing initializes OpenTelemetry tracing if configured.
// When tracing is not configured it returns a no-op cleanup function.
func setupTracing(cfg *config.Config) (func(context.Context) error, error) {
	if cfg.Metrics.OTLPEndpoint == "" {
		return func(context.Context) error { return nil }, nil
	}
	return initTracer(cfg)
}

// startBackgroundServices starts metrics and pprof servers
func startBackgroundServices(cfg *config.Config) {
	go func() {
		err := metrics.CreateMetricsServer(cfg)
		if err != nil {
			slog.Error("Failed to start metrics server", "error", err)
		}
	}()
	go pprof.CreatePProfServer(cfg)
}

// scheduleDailyUpdate starts an immediate background update and schedules a
// daily job for the given update function, from the URL that url returns
// at each run.
func scheduleDailyUpdate(scheduler gocron.Scheduler, name string, url func() string, updateFn func(string) error) {
	go func() {
		err := updateFn(url())
		if err != nil {
			slog.Error(fmt.Sprintf("Failed to update %s database, using built in one", name), "error", err)
		}
	}()

	_, err := scheduler.NewJob(
		gocron.DailyJob(1, gocron.NewAtTimes(
			gocron.NewAtTime(0, 0, 0),
		)),
		gocron.NewTask(func() {
			err := updateFn(url())
			if err != nil {
				slog.Error(fmt.Sprintf("Failed to update %s database", name), "error", err)
			}
		}),
	)
	if err != nil {
		slog.Error(fmt.Sprintf("Failed to schedule %s update", name), "error", err)
	}
}

// setupDMRDatabaseJobs configures scheduled jobs for database updates
func setupDMRDatabaseJobs(current config.Source, scheduler gocron.Scheduler) {
	scheduleDailyUpdate(scheduler, "repeater", func() string { return current.Get().DMR.RepeaterIDURL }, repeaterdb.Update)
	scheduleDailyUpdate(scheduler, "user", func() string { return current.Get().DMR.RadioIDURL }, userdb.Update)
}

// serverManager holds all the server instances and their dependencies
type serverManager struct {
	dmr          *manager.Manager
	httpServer   *http.Server
	netScheduler *netscheduler.NetScheduler
	callTracker  *calltracker.CallTracker
	messageStore *messages.Store
	purger       *retention.Purger
	aprs         *positions.Gateway
	plugins      *plugins.Server
	inside       *inside.Manager
	kv           kv.KV
	pubsub       pubsub.PubSub
	database     *gorm.DB
	cfg          config.Source
	live         *liveconfig.Live
	registry     *servers.InstanceRegistry
	ready        *atomic.Bool
}

// stopDMRServers sends disconnect messages (MSTCL, deregistration) and
// closes protocol sockets. This must run before hub.Stop() so that
// connected repeaters/peers receive a clean disconnect.
//
// When other DMRHub instances are alive (detected via the instance registry),
// disconnect messages are skipped so peers seamlessly migrate to a surviving
// instance instead of entering a slow re-registration cycle.
func (sm *serverManager) stopDMRServers(ctx context.Context) {
	graceful := false
	if sm.registry != nil {
		graceful = sm.registry.OtherServingInstanceExists(ctx)
		if graceful {
			slog.Info("Other DMRHub instances detected, performing graceful handoff (skipping disconnect messages)")
		} else {
			slog.Info("No other DMRHub instances detected, sending disconnect messages")
		}
		sm.registry.Deregister(ctx)
	}
	sm.dmr.Stop(servers.WithGracefulHandoff(ctx, graceful))
}

// drain keeps this replica serving while the other replicas take its
// sessions over, as manager.Manager.Drain does. The plugins move to the
// other replicas in the same time: those inside DMRHub stop here, and
// another replica starts them.
func (sm *serverManager) drain(ctx context.Context) {
	if sm.registry == nil {
		return
	}
	timeout := time.Duration(sm.cfg.Get().DMR.DrainTimeout) * time.Second
	var wg sync.WaitGroup
	if sm.plugins != nil && timeout > 0 && sm.registry.OtherInstancesExist(ctx) {
		pctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		wg.Go(func() { inside.Drain(pctx, sm.inside, sm.plugins) })
	}
	sm.dmr.Drain(ctx, sm.registry, timeout)
	wg.Wait()
}

// liveConfig puts the settings that the Setup page saves into effect: the
// DMR servers and the log level.
type liveConfig struct {
	dmr    *manager.Manager
	inside insideSwitch
}

// insideSwitch turns the plugins inside DMRHub on and off.
type insideSwitch interface {
	SetOn(ctx context.Context, on bool) error
}

func (l liveConfig) Apply(next *config.Config) []liveconfig.ApplyError {
	errs := l.dmr.Apply(next)
	setupLogger(next)
	if l.inside != nil {
		if err := l.inside.SetOn(context.Background(), next.DMR.Plugins.InProcess); err != nil {
			slog.Error("Plugins inside DMRHub could not take the new setting", "error", err)
		}
	}
	return manager.LiveErrors(errs)
}

// closeResources tears down the HTTP server, pubsub, and KV connections.
// Call this after hub.Stop() has cancelled all subscriptions.
func (sm *serverManager) closeResources(ctx context.Context) {
	if sm.live != nil {
		sm.live.Stop(ctx)
	}
	if sm.callTracker != nil {
		sm.callTracker.Stop()
	}
	if sm.messageStore != nil {
		sm.messageStore.Stop()
	}
	if sm.netScheduler != nil {
		sm.netScheduler.Stop()
	}
	if sm.aprs != nil {
		sm.aprs.Stop()
	}
	if sm.inside != nil {
		sm.inside.Stop(ctx)
	}
	sm.plugins.Stop()
	if sm.purger != nil {
		sm.purger.Stop()
	}
	sm.httpServer.Stop(ctx)
	if sm.pubsub != nil {
		if err := sm.pubsub.Close(); err != nil {
			slog.Error("Failed to close pubsub", "error", err)
		}
	}
	if sm.kv != nil {
		if err := sm.kv.Close(); err != nil {
			slog.Error("Failed to close kv", "error", err)
		}
	}
}

// shutdown gracefully stops all servers
func (sm *serverManager) shutdown(ctx context.Context) {
	sm.ready.Store(false)
	sm.stopDMRServers(ctx)
	sm.closeResources(ctx)
}

// initializeServers creates and starts all server instances
func initializeServers(ctx context.Context, live *liveconfig.Live, hub *hub.Hub, ct *calltracker.CallTracker, kvStore kv.KV, pubsubClient pubsub.PubSub, database *gorm.DB, version, commit string) (*serverManager, error) {
	instanceID, err := servers.GenerateInstanceID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate instance ID: %w", err)
	}
	registry := servers.NewInstanceRegistry(ctx, kvStore, instanceID)

	// Let the hub know our instance ID so it can track peer ownership in KV.
	if err := hub.SetInstanceID(instanceID); err != nil {
		return nil, fmt.Errorf("failed to set the instance ID: %w", err)
	}

	ready := &atomic.Bool{}
	current := live.Current()
	cfg := current.Get()

	sm := &serverManager{
		callTracker: ct,
		kv:          kvStore,
		pubsub:      pubsubClient,
		database:    database,
		cfg:         current,
		live:        live,
		registry:    registry,
		ready:       ready,
	}

	sm.dmr = manager.New(manager.Deps{Hub: hub, DB: database, PubSub: pubsubClient, KV: kvStore, Version: version, Commit: commit})
	if err := sm.dmr.Start(ctx, cfg); err != nil {
		return nil, fmt.Errorf("failed to start servers: %w", err)
	}

	ns := netscheduler.NewNetScheduler(database, pubsubClient, kvStore)
	if err := ns.LoadScheduledNets(ctx); err != nil {
		slog.Error("Failed to load scheduled nets", "error", err)
	}
	ns.Start()

	sm.netScheduler = ns

	sm.purger = retention.NewPurger(current, database, kvStore)
	sm.purger.Start(ctx)
	sm.aprs = positions.NewGateway(database, current, kvStore, pubsubClient, version)
	sm.aprs.Start(ctx)
	sm.plugins, err = plugins.Start(ctx, cfg, database, pubsubClient, hub, func(r *plugins.Registry) { hub.SetPlugins(r) },
		positions.NewPusher(database, pubsubClient).Push)
	if err != nil {
		return nil, fmt.Errorf("failed to start the plugin listener: %w", err)
	}
	applier := liveConfig{dmr: sm.dmr}
	pluginEngine := wasm.NewEngine(wasm.DefaultLimits())
	if sm.plugins != nil {
		sm.inside = inside.NewManager(inside.Deps{
			DB: database, PubSub: pubsubClient, KV: kvStore, Instance: instanceID, Version: version,
			Service: sm.plugins.Service(), Registry: sm.plugins.Registry(), Engine: pluginEngine,
		})
		if err := sm.inside.Start(ctx, cfg.DMR.Plugins.InProcess); err != nil {
			return nil, fmt.Errorf("failed to start the plugins inside DMRHub: %w", err)
		}
		applier.inside = sm.inside
	}

	live.Start(ctx, liveconfig.Replica{ID: instanceID, PubSub: pubsubClient, KV: kvStore, Applier: applier})

	httpServer := http.MakeServer(ctx, cfg, hub, database, pubsubClient, ready, ns, version, commit, http.WithLiveConfig(live), http.WithPluginEngine(pluginEngine), http.WithKV(kvStore))
	err = httpServer.Start()
	if err != nil {
		return nil, fmt.Errorf("failed to start HTTP server: %w", err)
	}
	sm.httpServer = &httpServer

	ready.Store(true)
	slog.Info("Server ready to accept traffic")

	return sm, nil
}

func setupShutdownHandlers(ctx context.Context, scheduler gocron.Scheduler, hub *hub.Hub, servers *serverManager, cleanup func(context.Context) error) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)

	sig := <-sigCh
	slog.Error("Shutting down due to signal", "signal", sig)

	// Mark unhealthy immediately so Kubernetes stops routing traffic.
	servers.ready.Store(false)

	servers.drain(ctx)

	// Stop the scheduler (independent of DMR shutdown).
	go func() {
		if err := scheduler.StopJobs(); err != nil {
			slog.Error("Failed to stop scheduler jobs", "error", err)
		}
		if err := scheduler.Shutdown(); err != nil {
			slog.Error("Failed to stop scheduler", "error", err)
		}
	}()

	hub.StartDraining()

	// Wait for all active calls to complete.
	// No timeout — we wait as long as needed.
	hub.WaitForCalls()

	// All calls are done. Now tear everything down.
	servers.stopDMRServers(ctx)
	hub.Stop(ctx)
	servers.closeResources(ctx)

	if cleanup != nil {
		const timeout = 5 * time.Second
		shutdownCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		if err := cleanup(shutdownCtx); err != nil {
			slog.Error("Failed to shutdown tracer", "error", err)
		}
	}

	slog.Info("All servers stopped, shutting down gracefully")
}

func initTracer(config *config.Config) (func(context.Context) error, error) {
	exporter, err := otlptrace.New(
		context.Background(),
		otlptracegrpc.NewClient(
			otlptracegrpc.WithInsecure(),
			otlptracegrpc.WithEndpoint(config.Metrics.OTLPEndpoint),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}
	resources, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			attribute.String("service.name", "DMRHub"),
			attribute.String("library.language", "go"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace resources: %w", err)
	}

	otel.SetTracerProvider(
		sdktrace.NewTracerProvider(
			sdktrace.WithSampler(sdktrace.AlwaysSample()),
			sdktrace.WithBatcher(exporter),
			sdktrace.WithResource(resources),
		),
	)
	return exporter.Shutdown, nil
}
