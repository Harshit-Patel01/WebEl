package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/opendeploy/opendeploy/internal/api"
	"github.com/opendeploy/opendeploy/internal/config"
	"github.com/opendeploy/opendeploy/internal/exec"
	"github.com/opendeploy/opendeploy/internal/services"
	"github.com/opendeploy/opendeploy/internal/state"
	"github.com/opendeploy/opendeploy/internal/ws"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func main() {
	configPath := flag.String("config", "/etc/opendeploy/config.yaml", "Path to config file")
	showVersion := flag.Bool("version", false, "Show version info")
	flag.Parse()

	if *showVersion {
		fmt.Printf("opendeploy %s (commit: %s, built: %s)\n", Version, Commit, BuildTime)
		os.Exit(0)
	}

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	logger := initLogger(cfg.Logging.Level)
	defer logger.Sync()

	logger.Info("starting opendeploy",
		zap.String("version", Version),
		zap.String("commit", Commit),
		zap.Int("port", cfg.Server.Port),
	)

	// Determine database directory
	dbDir := cfg.Database.Path
	for i := len(cfg.Database.Path) - 1; i >= 0; i-- {
		if cfg.Database.Path[i] == '/' {
			dbDir = cfg.Database.Path[:i]
			break
		}
	}

	// Ensure directories exist
	dirs := []string{
		cfg.Logging.LogDir,
		cfg.Deploy.WorkspaceRoot,
		dbDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			logger.Warn("failed to create directory", zap.String("dir", dir), zap.Error(err))
		}
	}

	// Initialize database
	db, err := state.NewDB(cfg.Database.Path)
	if err != nil {
		logger.Fatal("failed to initialize database", zap.Error(err))
	}
	defer db.Close()

	logger.Info("database initialized", zap.String("path", cfg.Database.Path))

	// Initialize WebSocket hub
	hub := ws.NewHub(logger)
	go hub.Run()

	// Initialize command runner
	runner := exec.NewRunner(hub, db, logger, cfg.Logging.LogDir)

	// Start system stats broadcaster
	go hub.StartStatsBroadcaster(5 * time.Second)

	// Start WiFi monitor for fallback AP
	wifiSvc := services.NewWifiService(runner, logger, db)
	wifiMonitor := services.NewWifiMonitor(runner, wifiSvc, logger)
	monitorCtx, cancelMonitor := context.WithCancel(context.Background())
	go wifiMonitor.Start(monitorCtx)

	// Build router
	router := api.NewRouter(cfg, db, hub, runner, logger)

	// HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		logger.Info("server listening", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	<-done
	logger.Info("shutting down...")

	// Stop WiFi monitor
	cancelMonitor()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("server forced shutdown", zap.Error(err))
	}

	logger.Info("server stopped")
}

func initLogger(level string) *zap.Logger {
	var zapLevel zapcore.Level
	switch level {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "warn":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		zapLevel = zapcore.InfoLevel
	}

	cfg := zap.Config{
		Level:            zap.NewAtomicLevelAt(zapLevel),
		Encoding:         "json",
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:        "ts",
			LevelKey:       "level",
			NameKey:        "logger",
			CallerKey:      "caller",
			MessageKey:     "msg",
			StacktraceKey:  "stacktrace",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.LowercaseLevelEncoder,
			EncodeTime:     zapcore.ISO8601TimeEncoder,
			EncodeDuration: zapcore.SecondsDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		},
	}

	logger, err := cfg.Build()
	if err != nil {
		panic(fmt.Sprintf("failed to initialize logger: %v", err))
	}
	return logger
}
