package ingestao

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

const uplinkValido = `{
  "end_device_ids": {"device_id": "d1", "application_ids": {"application_id": "app"}, "dev_eui": "70B3D57ED00724FA"},
  "received_at": "2025-08-21T21:08:51.011075263Z",
  "uplink_message": {"f_cnt": 18, "decoded_payload": {"message": "temperatura,23.0,C"}}
}`

type brutaGravada struct {
	chave, topico string
	json          string
}
type rejeicao struct {
	topico, conteudo, motivo string
}

type armazenamentoFalso struct {
	chaves     map[string]bool
	brutas     []brutaGravada
	rejeitadas []rejeicao
	falhas     int // número de chamadas que ainda devem falhar
}

func novoFalso() *armazenamentoFalso { return &armazenamentoFalso{chaves: map[string]bool{}} }

func (a *armazenamentoFalso) InserirBruta(_ context.Context, chave, topico string, _ time.Time, json []byte) (bool, error) {
	if a.falhas > 0 {
		a.falhas--
		return false, errors.New("mongo indisponível")
	}
	if a.chaves[chave] {
		return true, nil
	}
	a.chaves[chave] = true
	a.brutas = append(a.brutas, brutaGravada{chave, topico, string(json)})
	return false, nil
}

func (a *armazenamentoFalso) InserirRejeitada(_ context.Context, topico string, _ time.Time, conteudo []byte, motivo string) error {
	a.rejeitadas = append(a.rejeitadas, rejeicao{topico, string(conteudo), motivo})
	return nil
}

func novoServico(a Armazenamento) *Servico {
	s := Novo(a, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Agora = func() time.Time { return time.Date(2025, 8, 21, 21, 9, 0, 0, time.UTC) }
	return s
}

func TestTratar_ValidaPersistePreservandoOriginal(t *testing.T) {
	arm := novoFalso()
	s := novoServico(arm)

	if err := s.Tratar(context.Background(), "v3/app/devices/d1/up", []byte(uplinkValido)); err != nil {
		t.Fatal(err)
	}
	if len(arm.brutas) != 1 || len(arm.rejeitadas) != 0 {
		t.Fatalf("brutas=%d rejeitadas=%d", len(arm.brutas), len(arm.rejeitadas))
	}
	got := arm.brutas[0]
	if got.json != uplinkValido {
		t.Error("o JSON original deve ser preservado sem alteração")
	}
	if got.chave != "70B3D57ED00724FA|2025-08-21T21:08:51.011075263Z|18" {
		t.Errorf("chave = %q", got.chave)
	}
}

func TestTratar_ReentregaNaoDuplica(t *testing.T) {
	arm := novoFalso()
	s := novoServico(arm)

	for i := 0; i < 3; i++ {
		if err := s.Tratar(context.Background(), "t", []byte(uplinkValido)); err != nil {
			t.Fatal(err)
		}
	}
	if len(arm.brutas) != 1 {
		t.Errorf("brutas = %d, quer 1", len(arm.brutas))
	}
}

func TestTratar_InvalidaVaiParaRejeitadasSemErro(t *testing.T) {
	arm := novoFalso()
	s := novoServico(arm)

	if err := s.Tratar(context.Background(), "t", []byte("lixo")); err != nil {
		t.Fatalf("mensagem inválida não deve ser erro: %v", err)
	}
	if len(arm.brutas) != 0 || len(arm.rejeitadas) != 1 {
		t.Fatalf("brutas=%d rejeitadas=%d", len(arm.brutas), len(arm.rejeitadas))
	}
	if arm.rejeitadas[0].conteudo != "lixo" || arm.rejeitadas[0].motivo == "" {
		t.Errorf("rejeição incompleta: %+v", arm.rejeitadas[0])
	}
}

func TestTratar_FalhaDePersistenciaRetornaErro(t *testing.T) {
	arm := novoFalso()
	arm.falhas = 1
	s := novoServico(arm)

	if err := s.Tratar(context.Background(), "t", []byte(uplinkValido)); err == nil {
		t.Error("esperava erro quando o armazenamento falha")
	}
}

func TestTratarComRetentativas(t *testing.T) {
	t.Run("recupera após falhas transitórias", func(t *testing.T) {
		arm := novoFalso()
		arm.falhas = 2
		s := novoServico(arm)
		if err := s.TratarComRetentativas(context.Background(), "t", []byte(uplinkValido), 5, time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if len(arm.brutas) != 1 {
			t.Errorf("brutas = %d", len(arm.brutas))
		}
	})

	t.Run("desiste ao esgotar tentativas", func(t *testing.T) {
		arm := novoFalso()
		arm.falhas = 10
		s := novoServico(arm)
		if err := s.TratarComRetentativas(context.Background(), "t", []byte(uplinkValido), 3, time.Millisecond); err == nil {
			t.Error("esperava erro")
		}
		if arm.falhas != 7 {
			t.Errorf("falhas restantes = %d, esperado 7 (3 tentativas)", arm.falhas)
		}
	})

	t.Run("respeita cancelamento do contexto", func(t *testing.T) {
		arm := novoFalso()
		arm.falhas = 10
		s := novoServico(arm)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := s.TratarComRetentativas(ctx, "t", []byte(uplinkValido), 5, time.Hour); !errors.Is(err, context.Canceled) {
			t.Errorf("erro = %v, esperado context.Canceled", err)
		}
	})
}
