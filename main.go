package main

import (
	"context"
	"errors"
	"flag"
	gatewayserver "gateway/Internal/gateway"
	"gateway/Internal/handler"
	"gateway/Internal/sandbox"
	"gateway/Internal/session"
	"gateway/Internal/transports/socketio"
	"gateway/utils/config"
	"gateway/utils/log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	exitCode := 0
	defer func() {
		if exitCode != 0 {
			os.Exit(exitCode)
		}
	}()

	configPath := flag.String("config", "config.yaml", "Path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load configuration", "error", err)
		exitCode = 1
		return
	}
	logger := log.NewStandardLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	resolver, err := sandbox.NewEndpointResolver(cfg.Sandbox, nil)
	if err != nil {
		logger.Error("create sandbox resolver", "error", err)
		exitCode = 1
		return
	}

	manager := session.NewManager(ctx, resolver, &socketio.Connector{}, cfg.Server.BackendAddress)
	north := handler.NewNorthHandler(ctx, manager, cfg.Sandbox.AgentSelector, cfg.Sandbox.SandboxSelector)
	south := handler.NewSouthHandler(ctx, manager, cfg.Sandbox.AgentSelector)
	manager.SetHandlers(north, south)

	app := gatewayserver.NewServer(ctx, manager)
	defer func() {
		if err := app.Close(); err != nil {
			logger.Error("close gateway failed", "error", err)
		}
	}()

	server := &http.Server{
		Addr:              cfg.Server.GatewayAddress,
		Handler:           app,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("gateway server listening", "address", cfg.Server.GatewayAddress)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve gateway", "error", err)
			exitCode = 1
		}
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		logger.Error("shutdown HTTP server", "error", err)
		exitCode = 1
		return
	}

	if err := <-serverErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("serve gateway", "error", err)
		exitCode = 1
		return
	}

	logger.Info("gateway server stopped")
}
