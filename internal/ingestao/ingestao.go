// Package ingestao implementa a lógica do worker MQTT: validar a mensagem
// recebida e persisti-la no armazenamento bruto (MongoDB) ou registrá-la como
// rejeitada. O acesso ao banco fica atrás de uma interface para que a regra
// seja testável sem infraestrutura.
package ingestao

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"monitoramento/internal/contrato"
)

// Armazenamento é a camada de persistência bruta.
type Armazenamento interface {
	// InserirBruta grava o JSON original com status "pendente". Retorna
	// duplicada=true (sem erro) quando a chave de idempotência já existe.
	InserirBruta(ctx context.Context, chave, topico string, recebidaEm time.Time, json []byte) (duplicada bool, err error)
	// InserirRejeitada registra uma mensagem que falhou na validação (RF04).
	InserirRejeitada(ctx context.Context, topico string, recebidaEm time.Time, conteudo []byte, motivo string) error
}

// Servico valida e persiste as mensagens recebidas.
type Servico struct {
	arm Armazenamento
	log *slog.Logger
	// Agora fornece o relógio; substituível em testes.
	Agora func() time.Time
}

// Novo cria o serviço de ingestão.
func Novo(arm Armazenamento, log *slog.Logger) *Servico {
	return &Servico{arm: arm, log: log, Agora: time.Now}
}

// Tratar processa uma mensagem. Mensagens inválidas são registradas como
// rejeitadas e NÃO são erro (a mensagem foi tratada). Só falhas de
// persistência retornam erro, indicando que a mensagem ainda não está segura.
func (s *Servico) Tratar(ctx context.Context, topico string, payload []byte) error {
	agora := s.Agora().UTC()

	uplink, err := contrato.ParseUplink(payload)
	if err != nil {
		if errR := s.arm.InserirRejeitada(ctx, topico, agora, payload, err.Error()); errR != nil {
			return fmt.Errorf("registrar rejeição: %w", errR)
		}
		s.log.Warn("mensagem rejeitada", "topico", topico, "motivo", err.Error())
		return nil
	}

	duplicada, err := s.arm.InserirBruta(ctx, uplink.ChaveIdempotencia(), topico, agora, payload)
	if err != nil {
		return fmt.Errorf("persistir mensagem bruta: %w", err)
	}
	if duplicada {
		s.log.Debug("reentrega ignorada", "chave", uplink.ChaveIdempotencia())
	}
	return nil
}

// TratarComRetentativas repete Tratar com espera exponencial (espera, 2x, 4x...)
// até obter sucesso ou esgotar as tentativas. Serve para absorver quedas
// curtas do MongoDB antes de confirmar (ACK) a mensagem ao broker.
func (s *Servico) TratarComRetentativas(ctx context.Context, topico string, payload []byte, tentativas int, espera time.Duration) error {
	var err error
	for i := 0; i < tentativas; i++ {
		if err = s.Tratar(ctx, topico, payload); err == nil {
			return nil
		}
		if i == tentativas-1 {
			break
		}
		s.log.Warn("falha ao persistir mensagem; nova tentativa", "tentativa", i+1, "erro", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(espera << i):
		}
	}
	return err
}
