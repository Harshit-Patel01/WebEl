package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
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

	// Initialize WiFi service and AP
	wifiSvc := services.NewWifiService(runner, logger, db)
	wifiAP := services.NewWifiAP(runner, logger, db)

	// Run startup health checks and self-healing
	avahiSvc := services.NewAvahiService(runner, logger)
	go runStartupHealthChecks(logger, runner, avahiSvc, wifiSvc)

	// Ensure AP is running on startup (idempotent)
	apCtx, cancelAP := context.WithCancel(context.Background())
	go func() {
		// Wait for system to stabilize before starting AP
		time.Sleep(5 * time.Second)
		wifiAP.EnsureAP(apCtx)
	}()

	// Build router
	router := api.NewRouter(cfg, db, hub, runner, logger, wifiAP)

	// HTTP server — WriteTimeout is set high to support SSE streaming connections
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	sseWriteTimeout := 0 * time.Second // 0 = no timeout for SSE
	if cfg.Server.WriteTimeout > 0 && cfg.Server.WriteTimeout < 30*time.Minute {
		sseWriteTimeout = 30 * time.Minute
	}
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: sseWriteTimeout,
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

	// Stop AP service
	cancelAP()

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

// runStartupHealthChecks performs self-healing checks on startup
func runStartupHealthChecks(logger *zap.Logger, runner *exec.Runner, avahiSvc *services.AvahiService, wifiSvc *services.WifiService) {
	logger.Info("Starting system health checks and self-healing")

	// Wait a bit for system to stabilize
	time.Sleep(3 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1. Check Avahi daemon status (don't restart it)
	logger.Info("Checking Avahi daemon status")
	avahiResult, _ := runner.Run(ctx, exec.RunOpts{
		JobType: "check_avahi",
		Command: "sudo",
		Args:    []string{"systemctl", "is-active", "avahi-daemon"},
		Timeout: 5 * time.Second,
	})

	avahiActive := false
	if avahiResult != nil && len(avahiResult.Lines) > 0 {
		for _, line := range avahiResult.Lines {
			if line.Stream == "stdout" && strings.TrimSpace(line.Text) == "active" {
				avahiActive = true
				break
			}
		}
	}

	if avahiActive {
		logger.Info("Avahi daemon is active - hostname resolution available")
		// Only configure if needed, don't restart or refresh
		if err := avahiSvc.EnsureOptimalConfig(ctx); err != nil {
			logger.Warn("Failed to configure Avahi", zap.Error(err))
		}
	} else {
		logger.Warn("Avahi daemon not active - hostname resolution may not work")
		logger.Info("To enable hostname resolution, run: sudo systemctl start avahi-daemon")
	}

	// 2. Check WiFi status
	logger.Info("Checking WiFi connectivity")
	wifiStatus, err := wifiSvc.GetStatus(ctx)
	if err != nil {
		logger.Warn("Failed to get WiFi status", zap.Error(err))
	} else if wifiStatus.Connected {
		logger.Info("WiFi connected",
			zap.String("ssid", wifiStatus.SSID),
			zap.String("ip", wifiStatus.IP))
	} else {
		logger.Info("WiFi not connected - WiFi monitor will handle AP fallback if needed")
	}

	// 3. Check NetworkManager status
	logger.Info("Checking NetworkManager status")
	nmResult, _ := runner.Run(ctx, exec.RunOpts{
		JobType: "check_nm",
		Command: "sudo",
		Args:    []string{"systemctl", "is-active", "NetworkManager"},
		Timeout: 5 * time.Second,
	})

	nmActive := false
	if nmResult != nil && len(nmResult.Lines) > 0 {
		for _, line := range nmResult.Lines {
			if line.Stream == "stdout" && strings.TrimSpace(line.Text) == "active" {
				nmActive = true
				break
			}
		}
	}

	if !nmActive {
		logger.Warn("NetworkManager not active, attempting to start")
		_, err := runner.Run(ctx, exec.RunOpts{
			JobType: "start_nm",
			Command: "sudo",
			Args:    []string{"systemctl", "start", "NetworkManager"},
			Timeout: 10 * time.Second,
		})
		if err != nil {
			logger.Error("Failed to start NetworkManager", zap.Error(err))
		} else {
			logger.Info("NetworkManager started successfully")
		}
	}

	// 4. Ensure wlan0 is managed by NetworkManager
	logger.Info("Ensuring wlan0 is managed by NetworkManager")
	_, err = runner.Run(ctx, exec.RunOpts{
		JobType: "nm_manage_wlan0",
		Command: "sudo",
		Args:    []string{"nmcli", "device", "set", "wlan0", "managed", "yes"},
		Timeout: 5 * time.Second,
	})
	if err != nil {
		logger.Warn("Failed to set wlan0 as managed", zap.Error(err))
	}

	logger.Info("System health checks completed - backend is ready and self-sufficient")
}
