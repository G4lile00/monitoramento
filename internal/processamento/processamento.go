// Package processamento implementa o worker que lê documentos pendentes da
// fila (MongoDB), transforma as medições e as grava de forma idempotente no
// PostgreSQL. Fila e repositório ficam atrás de interfaces.
package processamento

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"monitoramento/internal/contrato"
)

// ErrPermanente marca falhas que não se resolvem com nova tentativa (por
// exemplo, mensagem malformada ou grandeza não cadastrada). Erros que a
// envolvem levam o documento direto ao estado "erro".
var ErrPermanente = errors.New("erro permanente")

// Item é um documento reservado para processamento.
type Item struct {
	ID         string // _id do documento em mensagens_brutas (hex)
	Tentativas int    // inclui a tentativa atual
	JSON       []byte // payload original em JSON
}

// Lote é o conjunto de leituras derivado de um único documento bruto.
type Lote struct {
	ApplicationID string
	DeviceID      string
	DevEUI        string
	JoinEUI       string
	MedidoEm      time.Time
	MongoID       string
	Medicoes      []contrato.Medicao
}

// Fila é a fila de trabalho persistente (MongoDB).
type Fila interface {
	// Reservar pega atomicamente o documento pendente (ou com reserva vencida)
	// mais antigo. Retorna nil, nil quando não há trabalho.
	Reservar(ctx context.Context, workerID string, validade time.Duration) (*Item, error)
	// Concluir marca o documento como processado.
	Concluir(ctx context.Context, id, workerID string) error
	// Falhar registra a falha. Se definitivo, o estado vira "erro"; caso
	// contrário o documento volta a "pendente" para nova tentativa.
	Falhar(ctx context.Context, id, workerID, motivo string, definitivo bool) error
}

// Repositorio grava as leituras estruturadas (PostgreSQL).
type Repositorio interface {
	// GravarLeituras deve ser idempotente: regravar o mesmo Lote não duplica.
	GravarLeituras(ctx context.Context, l Lote) error
}

// Config parametriza o worker.
type Config struct {
	Validade      time.Duration // validade da reserva
	Intervalo     time.Duration // espera quando a fila está vazia ou há erro
	MaxTentativas int
}

// Processador consome a fila.
type Processador struct {
	Fila Fila
	Repo Repositorio
	Cfg  Config
	Log  *slog.Logger
}

// Executar processa documentos em laço até o contexto ser cancelado.
func (p *Processador) Executar(ctx context.Context, workerID string) {
	p.Log.Info("worker iniciado", "worker", workerID)
	defer p.Log.Info("worker encerrado", "worker", workerID)

	for ctx.Err() == nil {
		item, err := p.Fila.Reservar(ctx, workerID, p.Cfg.Validade)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			p.Log.Error("falha ao reservar documento", "worker", workerID, "erro", err)
			dormir(ctx, p.Cfg.Intervalo)
			continue
		}
		if item == nil {
			dormir(ctx, p.Cfg.Intervalo)
			continue
		}
		p.Tratar(ctx, workerID, item)
	}
}

// Tratar processa um item já reservado e atualiza seu estado na fila.
func (p *Processador) Tratar(ctx context.Context, workerID string, item *Item) {
	err := p.processar(ctx, item)

	if err == nil {
		if e := p.Fila.Concluir(ctx, item.ID, workerID); e != nil {
			p.Log.Error("falha ao marcar documento como processado", "id", item.ID, "erro", e)
		}
		return
	}

	// Em desligamento, deixa a reserva expirar em vez de contar uma falha.
	if ctx.Err() != nil {
		return
	}

	definitivo := errors.Is(err, ErrPermanente) || item.Tentativas >= p.Cfg.MaxTentativas
	p.Log.Warn("falha no processamento", "id", item.ID, "tentativa", item.Tentativas, "definitivo", definitivo, "erro", err)
	if e := p.Fila.Falhar(ctx, item.ID, workerID, err.Error(), definitivo); e != nil {
		p.Log.Error("falha ao registrar erro do documento", "id", item.ID, "erro", e)
	}
}

func (p *Processador) processar(ctx context.Context, item *Item) error {
	uplink, err := contrato.ParseUplink(item.JSON)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPermanente, err)
	}
	medicoes, err := uplink.Medicoes()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPermanente, err)
	}

	ids := uplink.EndDeviceIDs
	return p.Repo.GravarLeituras(ctx, Lote{
		ApplicationID: ids.ApplicationIDs.ApplicationID,
		DeviceID:      ids.DeviceID,
		DevEUI:        ids.DevEUI,
		JoinEUI:       ids.JoinEUI,
		MedidoEm:      uplink.MedidoEm(),
		MongoID:       item.ID,
		Medicoes:      medicoes,
	})
}

func dormir(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
