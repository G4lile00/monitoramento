package fonte_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"tcc-monitoramento/internal/fonte"
	"tcc-monitoramento/internal/ingestao"
)

func TestLer_Formatos(t *testing.T) {
	casos := map[string]struct {
		entrada string
		quer    int
	}{
		"array":        {`[{"a":1},{"a":2}]`, 2},
		"objeto único": {`{"a":1}`, 1},
		"NDJSON":       {"{\"a\":1}\n{\"a\":2}\n{\"a\":3}\n", 3},
		"espaços":      {"  \n [ {\"a\":1} ] \n", 1},
		"array vazio":  {`[]`, 0},
	}
	for nome, c := range casos {
		t.Run(nome, func(t *testing.T) {
			msgs, err := fonte.Ler(strings.NewReader(c.entrada))
			if err != nil {
				t.Fatal(err)
			}
			if len(msgs) != c.quer {
				t.Errorf("mensagens = %d, quer %d", len(msgs), c.quer)
			}
		})
	}
}

func TestLer_PreservaOConteudoDeCadaMensagem(t *testing.T) {
	msgs, err := fonte.Ler(strings.NewReader(`[ {"a": 1,  "b":"x"} ]`))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(msgs[0]); got != `{"a": 1,  "b":"x"}` {
		t.Errorf("mensagem alterada: %q", got)
	}
}

func TestLer_Erros(t *testing.T) {
	for nome, entrada := range map[string]string{
		"vazio":             "  \n",
		"array malformado":  `[{"a":1},`,
		"objeto malformado": `{"a":1} {"b":`,
		"texto solto":       `olá`,
	} {
		t.Run(nome, func(t *testing.T) {
			if _, err := fonte.Ler(strings.NewReader(entrada)); err == nil {
				t.Error("esperava erro")
			}
		})
	}
}

// armazenamentoFalso conta o que a ingestão gravaria, sem precisar do MongoDB.
type armazenamentoFalso struct {
	chaves     map[string]bool
	brutas     int
	rejeitadas int
}

func (a *armazenamentoFalso) InserirBruta(_ context.Context, chave, _ string, _ time.Time, _ []byte) (bool, error) {
	if a.chaves[chave] {
		return true, nil
	}
	a.chaves[chave] = true
	a.brutas++
	return false, nil
}

func (a *armazenamentoFalso) InserirRejeitada(context.Context, string, time.Time, []byte, string) error {
	a.rejeitadas++
	return nil
}

// O arquivo de exemplo tem 6 mensagens: 4 válidas únicas, 1 duplicada
// (mesma chave de idempotência) e 1 sem dev_eui (rejeitada).
func TestArquivoDeExemplo_PelaIngestao(t *testing.T) {
	f, err := os.Open("../../testdata/mensagens_exemplo.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	msgs, err := fonte.Ler(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 6 {
		t.Fatalf("mensagens lidas = %d, quer 6", len(msgs))
	}

	arm := &armazenamentoFalso{chaves: map[string]bool{}}
	svc := ingestao.Novo(arm, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i, m := range msgs {
		if err := svc.Tratar(context.Background(), "arquivo/local", m); err != nil {
			t.Fatalf("mensagem %d: %v", i+1, err)
		}
	}

	if arm.brutas != 4 || arm.rejeitadas != 1 {
		t.Errorf("brutas=%d rejeitadas=%d, quer 4 e 1", arm.brutas, arm.rejeitadas)
	}
}
