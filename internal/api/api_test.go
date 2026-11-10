package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"monitoramento/internal/modelo"
)

type consultasFalsas struct {
	pingErr  error
	err      error
	sensores []modelo.Sensor
	leituras []modelo.Leitura
	filtro   modelo.FiltroLeituras
}

func (c *consultasFalsas) Pingar(context.Context) error { return c.pingErr }
func (c *consultasFalsas) ListarSensores(context.Context) ([]modelo.Sensor, error) {
	return c.sensores, c.err
}
func (c *consultasFalsas) ConsultarLeituras(_ context.Context, f modelo.FiltroLeituras) ([]modelo.Leitura, error) {
	c.filtro = f
	return c.leituras, c.err
}

var agoraFixo = time.Date(2025, 8, 22, 12, 0, 0, 0, time.UTC)

func chamar(t *testing.T, c Consultas, metodo, alvo string) *httptest.ResponseRecorder {
	t.Helper()
	h := novo(c, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return agoraFixo })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(metodo, alvo, nil))
	return rec
}

func TestSaude(t *testing.T) {
	if rec := chamar(t, &consultasFalsas{}, "GET", "/saude"); rec.Code != 200 {
		t.Errorf("status = %d", rec.Code)
	}
	if rec := chamar(t, &consultasFalsas{pingErr: errors.New("x")}, "GET", "/saude"); rec.Code != 503 {
		t.Errorf("status = %d, quer 503", rec.Code)
	}
}

func TestSensores_ListaVaziaEhArray(t *testing.T) {
	rec := chamar(t, &consultasFalsas{}, "GET", "/sensores")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Body.String(); got != "[]\n" {
		t.Errorf("corpo = %q, quer []", got)
	}
}

func TestLeituras_FiltrosEPadroes(t *testing.T) {
	medido := time.Date(2025, 8, 21, 21, 8, 50, 0, time.UTC)
	c := &consultasFalsas{leituras: []modelo.Leitura{
		{DevEUI: "70B3D57ED00724FA", Grandeza: "temperatura", Unidade: "C", MedidoEm: medido, Valor: 23, MongoID: "abc"},
	}}

	rec := chamar(t, c, "GET", "/leituras?dev_eui=70b3d57ed00724fa&grandeza=Temperatura&limite=10")
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	f := c.filtro
	if f.DevEUI == nil || *f.DevEUI != "70B3D57ED00724FA" {
		t.Errorf("dev_eui não normalizado: %v", f.DevEUI)
	}
	if f.Grandeza == nil || *f.Grandeza != "temperatura" {
		t.Errorf("grandeza não normalizada: %v", f.Grandeza)
	}
	if f.Limite != 10 || !f.Ate.Equal(agoraFixo) || !f.De.Equal(agoraFixo.Add(-24*time.Hour)) {
		t.Errorf("filtro = %+v", f)
	}

	var corpo []modelo.Leitura
	if err := json.Unmarshal(rec.Body.Bytes(), &corpo); err != nil {
		t.Fatal(err)
	}
	if len(corpo) != 1 || corpo[0].MongoID != "abc" || corpo[0].Valor != 23 {
		t.Errorf("corpo = %+v", corpo)
	}
}

func TestLeituras_IntervaloExplicito(t *testing.T) {
	c := &consultasFalsas{}
	rec := chamar(t, c, "GET", "/leituras?de=2025-08-21T00:00:00Z&ate=2025-08-21T12:00:00Z")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if !c.filtro.De.Equal(time.Date(2025, 8, 21, 0, 0, 0, 0, time.UTC)) ||
		!c.filtro.Ate.Equal(time.Date(2025, 8, 21, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("filtro = %+v", c.filtro)
	}
}

func TestLeituras_ParametrosInvalidos(t *testing.T) {
	alvos := []string{
		"/leituras?de=ontem",
		"/leituras?ate=amanha",
		"/leituras?de=2025-08-22T13:00:00Z",                          // depois de 'ate' (agora)
		"/leituras?de=2025-08-21T00:00:00Z&ate=2025-08-21T00:00:00Z", // vazio
		"/leituras?limite=0",
		"/leituras?limite=abc",
		"/leituras?limite=1001",
	}
	for _, a := range alvos {
		t.Run(a, func(t *testing.T) {
			if rec := chamar(t, &consultasFalsas{}, "GET", a); rec.Code != 400 {
				t.Errorf("status = %d, quer 400", rec.Code)
			}
		})
	}
}

func TestLeituras_ErroInternoNaoVazaDetalhes(t *testing.T) {
	rec := chamar(t, &consultasFalsas{err: errors.New("senha do banco: hunter2")}, "GET", "/leituras")
	if rec.Code != 500 {
		t.Fatalf("status = %d", rec.Code)
	}
	if body := rec.Body.String(); body != "{\"erro\":\"erro interno\"}\n" {
		t.Errorf("corpo = %q", body)
	}
}

func TestMetodoNaoPermitido(t *testing.T) {
	if rec := chamar(t, &consultasFalsas{}, "POST", "/leituras"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, quer 405", rec.Code)
	}
}
