package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/elmhuangyu/mcp-bash/internal/config"
	"github.com/elmhuangyu/mcp-bash/internal/runner"
	"github.com/elmhuangyu/mcp-bash/internal/server"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg, err := config.LoadFromEnv()
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	r := runner.New(
		cfg.BashPath,
		cfg.HomeDir,
		cfg.DefaultTimeout,
		cfg.MaxTimeout,
		cfg.MaxOutputBytes,
	)
	if err := r.EnsureLocalBin(); err != nil {
		logger.Warn("failed to initialize $HOME/.local/bin", "error", err)
	}

	mcpServer := server.NewMCPServer(r, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if cfg.Transport == config.TransportStdio {
		logger.Info("starting mcp-bash server", "transport", cfg.Transport, "home", cfg.HomeDir)
		if err := mcpServer.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("stdio server exited with error", "error", err)
			os.Exit(1)
		}
		return
	}

	mux := server.NewHTTPMux(cfg, mcpServer, logger)
	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting mcp-bash server",
			"transport", cfg.Transport,
			"addr", cfg.Addr,
			"base_path", cfg.BasePath,
			"path", cfg.MCPRoute(),
			"health_path", cfg.HealthRoute(),
			"auth_enabled", cfg.AccessToken != "",
			"stateless", cfg.Stateless,
			"home", cfg.HomeDir,
		)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		if err != nil {
			logger.Error("HTTP server failed", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		logger.Info("shutting down mcp-bash server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("HTTP server shutdown error", "error", err)
			os.Exit(1)
		}
		if err := <-errCh; err != nil {
			logger.Error("HTTP server exit error", "error", err)
			os.Exit(1)
		}
	}
}
