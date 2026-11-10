// Comando processador lê documentos pendentes do MongoDB e grava as leituras
// estruturadas no PostgreSQL. Executa até receber SIGINT/SIGTERM.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"monitoramento/internal/config"
	"monitoramento/internal/mongodb"
	"monitoramento/internal/postgres"
	"monitoramento/internal/processamento"
)

func main() {
	if err := run(); err != nil {
		slog.Error("falha no processador", "erro", err)
		os.Exit(1)
	}
}

func run() error {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := config.CarregarProcessador()
	if err != nil {
		return fmt.Errorf("configuração inválida: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := mongodb.Conectar(ctx, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		return err
	}
	defer store.Fechar(context.Background())
	if err := store.GarantirIndices(ctx); err != nil {
		return err
	}

	repo, err := postgres.Conectar(ctx, cfg.DatabaseURL, cfg.AutoCadastrarSensor)
	if err != nil {
		return err
	}
	defer repo.Fechar()

	p := &processamento.Processador{
		Fila: store,
		Repo: repo,
		Log:  log,
		Cfg: processamento.Config{
			Validade:      cfg.Reserva,
			Intervalo:     cfg.Intervalo,
			MaxTentativas: cfg.MaxTentativas,
		},
	}

	host, _ := os.Hostname()
	var wg sync.WaitGroup
	for i := 1; i <= cfg.Workers; i++ {
		id := fmt.Sprintf("%s-%d-%d", host, os.Getpid(), i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Executar(ctx, id)
		}()
	}
	wg.Wait()
	return nil
}
