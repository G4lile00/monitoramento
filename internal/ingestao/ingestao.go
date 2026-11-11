// Package ingestao implementa a lógica do worker MQTT: validar a mensagem
// recebida e persisti-la no armazenamento bruto (MongoDB) ou registrá-la como
// rejeitada. O acesso ao banco fica atrás de uma interface para que a regra
// seja testável sem infraestrutura.
//
// O parser é injetável para permitir múltiplos contratos (TTN v3, ChirpStack
// v4). Por padrão usa contrato.ParseAutomatico, que detecta o formato pelo
// conteúdo; workers específicos podem injetar parsers dedicados.
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

// Parser converte o payload bruto em uma Mensagem. Retorna erro (que vira
// registro em mensagens_rejeitadas) quando o conteúdo não é reconhecido.
type Parser func([]byte) (contrato.Mensagem, error)

// Servico valida e persiste as mensagens recebidas.
type Servico struct {
	arm    Armazenamento
	log    *slog.Logger
	parser Parser
	// Agora fornece o relógio; substituível em testes.
	Agora func() time.Time
}

// Novo cria o serviço de ingestão com o parser automático (detecta TTN v3
// e ChirpStack v4 pelo conteúdo da mensagem).
func Novo(arm Armazenamento, log *slog.Logger) *Servico {
	return &Servico{arm: arm, log: log, parser: contrato.ParseAutomatico, Agora: time.Now}
}

// NovoComParser cria o serviço com um parser dedicado. Útil para testes
// ou para workers que sabem exatamente qual contrato esperam.
func NovoComParser(arm Armazenamento, log *slog.Logger, p Parser) *Servico {
	if p == nil {
		p = contrato.ParseAutomatico
	}
	return &Servico{arm: arm, log: log, parser: p, Agora: time.Now}
}

// Tratar processa uma mensagem. Mensagens inválidas são registradas como
// rejeitadas e NÃO são erro (a mensagem foi tratada). Só falhas de
// persistência retornam erro, indicando que a mensagem ainda não está segura.
func (s *Servico) Tratar(ctx context.Context, topico string, payload []byte) error {
	agora := s.Agora().UTC()

	msg, err := s.parser(payload)
	if err != nil {
		if errR := s.arm.InserirRejeitada(ctx, topico, agora, payload, err.Error()); errR != nil {
			return fmt.Errorf("registrar rejeição: %w", errR)
		}
		s.log.Warn("mensagem rejeitada", "topico", topico, "motivo", err.Error())
		return nil
	}

	duplicada, err := s.arm.InserirBruta(ctx, msg.ChaveIdempotencia(), topico, agora, payload)
	if err != nil {
		return fmt.Errorf("persistir mensagem bruta: %w", err)
	}
	if duplicada {
		s.log.Debug("reentrega ignorada", "chave", msg.ChaveIdempotencia())
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
