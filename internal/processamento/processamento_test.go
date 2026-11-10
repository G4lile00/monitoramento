package processamento

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

const uplinkValido = `{
  "end_device_ids": {"device_id": "d1", "application_ids": {"application_id": "app"}, "dev_eui": "70B3D57ED00724FA"},
  "received_at": "2025-08-21T21:08:51.011075263Z",
  "uplink_message": {
    "f_cnt": 18,
    "decoded_payload": {"message": "temperatura,23.0,C,umidade,60.0,%"},
    "settings": {"time": "2025-08-21T21:08:50.613867Z"}
  }
}`

type resultado struct {
	concluido  bool
	falhou     bool
	definitivo bool
	motivo     string
}

type filaFalsa struct {
	mu    sync.Mutex
	itens []*Item
	res   map[string]*resultado
}

func novaFila(itens ...*Item) *filaFalsa {
	return &filaFalsa{itens: itens, res: map[string]*resultado{}}
}

func (f *filaFalsa) Reservar(context.Context, string, time.Duration) (*Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.itens) == 0 {
		return nil, nil
	}
	it := f.itens[0]
	f.itens = f.itens[1:]
	return it, nil
}

func (f *filaFalsa) Concluir(_ context.Context, id, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.res[id] = &resultado{concluido: true}
	return nil
}

func (f *filaFalsa) Falhar(_ context.Context, id, _, motivo string, definitivo bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.res[id] = &resultado{falhou: true, definitivo: definitivo, motivo: motivo}
	return nil
}

type repoFalso struct {
	err   error
	lotes []Lote
}

func (r *repoFalso) GravarLeituras(_ context.Context, l Lote) error {
	if r.err != nil {
		return r.err
	}
	r.lotes = append(r.lotes, l)
	return nil
}

func novoProcessador(f Fila, r Repositorio) *Processador {
	return &Processador{
		Fila: f, Repo: r,
		Cfg: Config{Validade: time.Minute, Intervalo: time.Millisecond, MaxTentativas: 3},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestTratar_Sucesso(t *testing.T) {
	item := &Item{ID: "abc", Tentativas: 1, JSON: []byte(uplinkValido)}
	fila, repo := novaFila(), &repoFalso{}
	p := novoProcessador(fila, repo)

	p.Tratar(context.Background(), "w1", item)

	if !fila.res["abc"].concluido {
		t.Fatalf("documento deveria estar concluído: %+v", fila.res["abc"])
	}
	if len(repo.lotes) != 1 {
		t.Fatalf("lotes = %d", len(repo.lotes))
	}
	l := repo.lotes[0]
	if l.MongoID != "abc" || l.DevEUI != "70B3D57ED00724FA" || l.ApplicationID != "app" || l.DeviceID != "d1" {
		t.Errorf("lote incorreto: %+v", l)
	}
	if len(l.Medicoes) != 2 {
		t.Errorf("medições = %+v", l.Medicoes)
	}
	if want := time.Date(2025, 8, 21, 21, 8, 50, 613867000, time.UTC); !l.MedidoEm.Equal(want) {
		t.Errorf("MedidoEm = %v", l.MedidoEm)
	}
}

func TestTratar_MensagemIlegivelEhErroPermanente(t *testing.T) {
	casos := map[string]string{
		"JSON inválido":      `{`,
		"sem medições":       `{"end_device_ids":{"device_id":"d","dev_eui":"70B3D57ED00724FA","application_ids":{"application_id":"a"}},"received_at":"2025-08-21T21:08:51Z","uplink_message":{"decoded_payload":{}}}`,
		"valor não numérico": `{"end_device_ids":{"device_id":"d","dev_eui":"70B3D57ED00724FA","application_ids":{"application_id":"a"}},"received_at":"2025-08-21T21:08:51Z","uplink_message":{"decoded_payload":{"message":"temperatura,x,C"}}}`,
	}
	for nome, raw := range casos {
		t.Run(nome, func(t *testing.T) {
			fila, repo := novaFila(), &repoFalso{}
			p := novoProcessador(fila, repo)
			// primeira tentativa: mesmo assim deve ser definitivo
			p.Tratar(context.Background(), "w1", &Item{ID: "x", Tentativas: 1, JSON: []byte(raw)})

			r := fila.res["x"]
			if r == nil || !r.falhou || !r.definitivo {
				t.Fatalf("esperava falha definitiva: %+v", r)
			}
			if len(repo.lotes) != 0 {
				t.Error("não deveria gravar leituras")
			}
		})
	}
}

func TestTratar_FalhaTransitoriaVoltaParaPendente(t *testing.T) {
	fila := novaFila()
	p := novoProcessador(fila, &repoFalso{err: errors.New("postgres fora do ar")})

	p.Tratar(context.Background(), "w1", &Item{ID: "x", Tentativas: 1, JSON: []byte(uplinkValido)})

	r := fila.res["x"]
	if r == nil || !r.falhou || r.definitivo {
		t.Fatalf("esperava falha não definitiva: %+v", r)
	}
}

func TestTratar_EsgotaTentativas(t *testing.T) {
	fila := novaFila()
	p := novoProcessador(fila, &repoFalso{err: errors.New("postgres fora do ar")})

	p.Tratar(context.Background(), "w1", &Item{ID: "x", Tentativas: 3, JSON: []byte(uplinkValido)})

	if r := fila.res["x"]; r == nil || !r.definitivo {
		t.Fatalf("na última tentativa a falha deve ser definitiva: %+v", r)
	}
}

func TestTratar_ErroPermanenteDoRepositorio(t *testing.T) {
	fila := novaFila()
	err := fmt.Errorf("%w: grandeza %q não cadastrada", ErrPermanente, "ruido")
	p := novoProcessador(fila, &repoFalso{err: err})

	p.Tratar(context.Background(), "w1", &Item{ID: "x", Tentativas: 1, JSON: []byte(uplinkValido)})

	if r := fila.res["x"]; r == nil || !r.definitivo {
		t.Fatalf("erro permanente do repositório deve ser definitivo: %+v", r)
	}
}

func TestTratar_CancelamentoNaoRegistraFalha(t *testing.T) {
	fila := novaFila()
	ctx, cancel := context.WithCancel(context.Background())
	repo := &repoFalso{err: context.Canceled}
	p := novoProcessador(fila, repo)
	cancel()

	p.Tratar(ctx, "w1", &Item{ID: "x", Tentativas: 1, JSON: []byte(uplinkValido)})

	if _, ok := fila.res["x"]; ok {
		t.Error("durante o desligamento a reserva deve expirar, sem registrar falha")
	}
}

func TestExecutar_ConsomeFilaEEncerraComContexto(t *testing.T) {
	fila := novaFila(
		&Item{ID: "1", Tentativas: 1, JSON: []byte(uplinkValido)},
		&Item{ID: "2", Tentativas: 1, JSON: []byte(uplinkValido)},
	)
	repo := &repoFalso{}
	p := novoProcessador(fila, repo)

	ctx, cancel := context.WithCancel(context.Background())
	feito := make(chan struct{})
	go func() { p.Executar(ctx, "w1"); close(feito) }()

	deadline := time.After(2 * time.Second)
	for {
		fila.mu.Lock()
		n := len(fila.res)
		fila.mu.Unlock()
		if n == 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("tempo esgotado aguardando o processamento")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case <-feito:
	case <-time.After(2 * time.Second):
		t.Fatal("worker não encerrou após o cancelamento")
	}
}
