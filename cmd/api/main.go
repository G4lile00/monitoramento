// Comando api sobe a API REST de consulta aos dados estruturados.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"monitoramento/internal/api"
	"monitoramento/internal/config"
	"monitoramento/internal/postgres"
)

func main() {
	if err := run(); err != nil {
		slog.Error("falha na API", "erro", err)
		os.Exit(1)
	}
}

func run() error {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := config.CarregarAPI()
	if err != nil {
		return fmt.Errorf("configuração inválida: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repo, err := postgres.Conectar(ctx, cfg.DatabaseURL, false)
	if err != nil {
		return err
	}
	defer repo.Fechar()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.Handler(repo, log),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		desligar, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(desligar)
	}()

	log.Info("API escutando", "endereco", cfg.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
