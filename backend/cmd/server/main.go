package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/agentrq/sql-mcp/backend/internal/app"
	"github.com/agentrq/sql-mcp/backend/internal/config"
	"github.com/agentrq/sql-mcp/backend/internal/service/server"
)

func main() {
	zerolog.TimeFieldFormat = time.RFC3339
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("config")
	}
	handler, err := app.New(cfg, app.Deps{})
	if err != nil {
		log.Fatal().Err(err).Msg("app")
	}
	srv := server.New(server.Config{
		HTTPPort:           cfg.HTTPPort,
		HTTPSPort:          cfg.HTTPSPort,
		SSLEnabled:         cfg.SSLEnabled,
		Domain:             cfg.Domain,
		SSLCacheDir:        cfg.SSLCacheDir,
		LetsencryptEmail:   cfg.LetsencryptEmail,
		CloudflareAPIToken: cfg.CloudflareAPIToken,
		ACMEDirectoryURL:   cfg.ACMEDirectoryURL,
	}, handler)

	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	log.Info().Str("baseUrl", cfg.BaseURL).Str("version", app.Version).Msg("sql-mcp starting")
	if err := srv.Run(); err != nil {
		log.Fatal().Err(err).Msg("server")
	}
}
