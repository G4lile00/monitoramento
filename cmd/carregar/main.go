// Comando carregar lê mensagens de uplink de um arquivo JSON e as ingere no
// MongoDB pelo mesmo caminho que o worker MQTT usará (validação, chave de
// idempotência, mensagens rejeitadas). Serve para testar localmente sem broker.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"monitoramento/internal/config"
	"monitoramento/internal/fonte"
	"monitoramento/internal/ingestao"
	"monitoramento/internal/mongodb"
)

func main() {
	if err := run(); err != nil {
		slog.Error("falha na carga", "erro", err)
		os.Exit(1)
	}
}

func run() error {
	arquivo := flag.String("arquivo", "testdata/mensagens_exemplo.json", "arquivo JSON com as mensagens")
	topico := flag.String("topico", "arquivo/local", "valor gravado em topico_mqtt")
	flag.Parse()

	// Nível Debug para mostrar também as reentregas ignoradas.
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg, err := config.CarregarMongo()
	if err != nil {
		return fmt.Errorf("configuração inválida: %w", err)
	}

	f, err := os.Open(*arquivo)
	if err != nil {
		return err
	}
	defer f.Close()
	mensagens, err := fonte.Ler(f)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := mongodb.Conectar(ctx, cfg.URI, cfg.DB)
	if err != nil {
		return err
	}
	defer store.Fechar(context.Background())

	if err := store.GarantirIndices(ctx); err != nil {
		return err
	}

	svc := ingestao.Novo(store, log)
	for i, m := range mensagens {
		if err := svc.Tratar(ctx, *topico, m); err != nil {
			return fmt.Errorf("mensagem %d: %w", i+1, err)
		}
	}
	log.Info("carga concluída", "arquivo", *arquivo, "mensagens_lidas", len(mensagens))
	return nil
}
