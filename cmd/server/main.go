package main

import (
	"context"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mizupanel/mizupanel/internal/server/api"
	"github.com/mizupanel/mizupanel/internal/server/app"
	serveraudit "github.com/mizupanel/mizupanel/internal/server/audit"
	"github.com/mizupanel/mizupanel/internal/server/config"
	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
	"github.com/mizupanel/mizupanel/internal/server/logbuffer"
	"github.com/mizupanel/mizupanel/internal/server/retention"
	"github.com/mizupanel/mizupanel/internal/server/servicecenter"
	"github.com/mizupanel/mizupanel/internal/server/store"
	serveruptime "github.com/mizupanel/mizupanel/internal/server/uptime"
	"github.com/mizupanel/mizupanel/internal/version"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	handled, err := version.PrintCommand(os.Args[1:], "Server", os.Stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	serverLogs := logbuffer.New(logbuffer.DefaultMaxEntries, logbuffer.DefaultMaxBytes)
	log.SetOutput(io.MultiWriter(os.Stderr, serverLogs))

	configPath := flag.String("config", "", "path to server config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, dialect, err := serverdb.OpenStorage(cfg.Storage)
	if err != nil {
		return err
	}
	defer database.Close()

	nodes := store.NewNodeStoreWithDialect(database, dialect)
	if err := nodes.ResetOnlineStatuses(ctx, time.Now().UTC()); err != nil {
		return err
	}
	metrics := store.NewMetricStore(database)
	processSnapshots := store.NewProcessSnapshotStoreWithDialect(database, dialect)
	dockerSnapshots := store.NewDockerSnapshotStoreWithDialect(database, dialect)
	agentTokens := store.NewAgentTokenStoreWithDialect(database, dialect)
	settings := store.NewSettingsStoreWithDialect(database, dialect)
	alerts := store.NewAlertStore(database)
	uptimeMonitors := store.NewUptimeStoreWithDialect(database, dialect)
	tasks := store.NewTaskStoreWithDialect(database, dialect)
	services := servicecenter.NewStore(database, dialect)
	aiStore := store.NewAIStore(database, dialect)
	auditStore := serveraudit.NewStore(database, dialect)
	cleaner := retention.NewDynamicCleaner(metrics, func() (time.Duration, error) {
		return settings.MetricsRetention(ctx, cfg.MetricsRetention)
	})
	go cleaner.Run(ctx, cfg.CleanupInterval)
	auditCleaner := retention.NewAuditCleaner(auditStore, cfg.AuditRetention)
	go auditCleaner.Run(ctx, cfg.AuditCleanup)
	taskCleaner := retention.NewTaskRunCleaner(tasks, cfg.TaskRetention)
	go taskCleaner.Run(ctx, cfg.TaskCleanup)

	paths, err := runtimeReleasePaths(os.Executable)
	if err != nil {
		return err
	}
	if cfg.Debug {
		log.Printf("[debug][server] debug logging enabled")
	}
	handler := app.NewHandler(app.Dependencies{
		Context:             ctx,
		Nodes:               nodes,
		Metrics:             metrics,
		ProcessSnapshots:    processSnapshots,
		DockerSnapshots:     dockerSnapshots,
		AgentTokens:         agentTokens,
		Settings:            settings,
		Alerts:              alerts,
		Uptime:              uptimeMonitors,
		Tasks:               tasks,
		Services:            services,
		AI:                  aiStore,
		AIKeyFile:           cfg.AIKeyFile,
		AgentToken:          cfg.AgentToken,
		PublicURL:           cfg.PublicURL,
		Interval:            5,
		StaticDir:           paths.StaticDir,
		DownloadDir:         paths.DownloadDir,
		EnableTerminal:      cfg.EnableTerminal,
		MetricsRetention:    cfg.MetricsRetention,
		AlertingEnabled:     cfg.Alerting.Enabled,
		AlertCheckInterval:  cfg.Alerting.CheckInterval,
		UptimeSweepInterval: serveruptime.DefaultSweepInterval,
		K8sHealthEnabled:    true,
		Debug:               cfg.Debug,
		AdminAuth:           appAuthConfig(cfg.AdminAuth),
		Audit:               auditStore,
		ServerLogs:          serverLogs,
	})
	server := newHTTPServer(cfg.Listen, handler)
	serveErrors := make(chan error, 1)
	go func() {
		log.Printf("MizuPanel server listening on %s", cfg.Listen)
		serveErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serveErrors:
		if err == nil || err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = handler.Close()
		return err
	}
	if err := handler.Close(); err != nil {
		return err
	}
	if err := <-serveErrors; err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

const (
	serverReadHeaderTimeout = 5 * time.Second
	serverIdleTimeout       = 2 * time.Minute
	serverMaxHeaderBytes    = 1 << 20
)

func newHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		IdleTimeout:       serverIdleTimeout,
		MaxHeaderBytes:    serverMaxHeaderBytes,
	}
}

func appAuthConfig(cfg config.AdminAuthConfig) api.AuthConfig {
	return api.AuthConfig{
		Enabled:    cfg.Enabled,
		Username:   cfg.Username,
		Password:   cfg.Password,
		SessionTTL: cfg.SessionTTL,
	}
}

type releaseAssetPaths struct {
	StaticDir   string
	DownloadDir string
}

func runtimeReleasePaths(executablePath func() (string, error)) (releaseAssetPaths, error) {
	executable, err := executablePath()
	if err != nil {
		return releaseAssetPaths{}, err
	}
	absolute, err := filepath.Abs(executable)
	if err != nil {
		return releaseAssetPaths{}, err
	}
	return releasePaths(absolute), nil
}

func releasePaths(executable string) releaseAssetPaths {
	root := filepath.Dir(executable)
	return releaseAssetPaths{
		StaticDir:   filepath.Join(root, "web"),
		DownloadDir: filepath.Join(root, "downloads"),
	}
}
