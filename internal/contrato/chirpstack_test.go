package contrato

import (
	"strings"
	"testing"
	"time"
)

// Exemplo simplificado do uplink do ChirpStack v4 (sensor de deslizamento).
const exemploChirpStack = `{
  "deduplicationId": "833657f0-20de-4ad0-b56c-c06d776b0cd0",
  "time": "2026-10-21T17:11:58.016+00:00",
  "deviceInfo": {
    "applicationId": "4e97de35-d0ea-4b6d-8cfd-6a8cf88e9451",
    "applicationName": "TESTE_HELTEC_LORAWAN",
    "deviceName": "HELTEC_TESTE_017",
    "devEui": "60b4d64fa899e3eb",
    "tags": {
      "Latitude": "-26.917836273741372",
      "Longitude": "-48.66544228404989",
      "Altitude": "6.8 metros "
    }
  },
  "object": {
    "vibracao_pico_ms2": 1,
    "temperatura_c": 24.1,
    "bateria_v": 4.34,
    "umidade_pct": 49,
    "pressao_hpa": 1021,
    "inclinacao_sensor_deg": 155.48,
    "solo_pct": 7
  }
}`

func TestParseUplinkChirpStack_Exemplo(t *testing.T) {
	u, err := ParseUplinkChirpStack([]byte(exemploChirpStack))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if u.ApplicationID() != "4e97de35-d0ea-4b6d-8cfd-6a8cf88e9451" {
		t.Errorf("ApplicationID = %q", u.ApplicationID())
	}
	if u.DeviceID() != "HELTEC_TESTE_017" {
		t.Errorf("DeviceID = %q", u.DeviceID())
	}
	if u.DevEUI() != "60B4D64FA899E3EB" {
		t.Errorf("DevEUI = %q (esperado normalizado maiúsculas)", u.DevEUI())
	}
	if u.JoinEUI() != "" {
		t.Errorf("JoinEUI esperado vazio, veio %q", u.JoinEUI())
	}

	wantChave := "cs:833657f0-20de-4ad0-b56c-c06d776b0cd0"
	if got := u.ChaveIdempotencia(); got != wantChave {
		t.Errorf("chave = %q, quer %q", got, wantChave)
	}

	wantT := time.Date(2026, 10, 21, 17, 11, 58, 16_000_000, time.UTC)
	if got := u.MedidoEm(); !got.Equal(wantT) {
		t.Errorf("MedidoEm = %v, quer %v", got, wantT)
	}

	lat, lon, alt := u.Localizacao()
	if lat == nil || lon == nil {
		t.Fatalf("Localizacao não deveria ser nil")
	}
	if *lat < -26.92 || *lat > -26.91 {
		t.Errorf("Latitude = %v", *lat)
	}
	if *lon < -48.67 || *lon > -48.66 {
		t.Errorf("Longitude = %v", *lon)
	}
	if alt == nil || *alt != 6.8 {
		t.Errorf("Altitude = %v (esperado 6.8)", alt)
	}

	medicoes, err := u.Medicoes()
	if err != nil {
		t.Fatalf("Medicoes: %v", err)
	}
	if len(medicoes) != 7 {
		t.Fatalf("esperado 7 medições, veio %d: %+v", len(medicoes), medicoes)
	}
	// Confere algumas pelo codigo (a ordem não é garantida por vir de map).
	seen := map[string]Medicao{}
	for _, m := range medicoes {
		seen[m.Codigo] = m
	}
	if seen["vibracao"] != (Medicao{"vibracao", 1, "m/s²"}) {
		t.Errorf("vibracao = %+v", seen["vibracao"])
	}
	if seen["inclinacao"] != (Medicao{"inclinacao", 155.48, "°"}) {
		t.Errorf("inclinacao = %+v", seen["inclinacao"])
	}
	if seen["pressao"] != (Medicao{"pressao", 1021, "hPa"}) {
		t.Errorf("pressao = %+v", seen["pressao"])
	}
	if seen["solo"] != (Medicao{"solo", 7, "%"}) {
		t.Errorf("solo = %+v", seen["solo"])
	}
}

func TestParseUplinkChirpStack_DevEUINormalizado(t *testing.T) {
	raw := strings.Replace(exemploChirpStack, "60b4d64fa899e3eb", "60B4D64FA899E3EB", 1)
	u, err := ParseUplinkChirpStack([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if u.DevEUI() != "60B4D64FA899E3EB" {
		t.Errorf("dev_eui = %q", u.DevEUI())
	}
}

func TestParseUplinkChirpStack_Rejeicoes(t *testing.T) {
	casos := []struct {
		nome, raw, motivo string
	}{
		{"não é JSON", `não sou json`, "JSON inválido"},
		{"array", `[]`, "JSON inválido"},
		{"vazio", `{}`, "deduplicationId"},
		{
			"sem deduplicationId",
			strings.Replace(exemploChirpStack, `"deduplicationId": "833657f0-20de-4ad0-b56c-c06d776b0cd0",`, "", 1),
			"deduplicationId",
		},
		{
			"sem devEui",
			strings.Replace(exemploChirpStack, `"devEui": "60b4d64fa899e3eb",`, "", 1),
			"devEui",
		},
		{
			"devEui curto",
			strings.Replace(exemploChirpStack, "60b4d64fa899e3eb", "60b4", 1),
			"devEui",
		},
		{
			"sem applicationId",
			strings.Replace(exemploChirpStack, `"applicationId": "4e97de35-d0ea-4b6d-8cfd-6a8cf88e9451",`, "", 1),
			"applicationId",
		},
		{
			"sem time",
			strings.Replace(exemploChirpStack, `"time": "2026-10-21T17:11:58.016+00:00",`, "", 1),
			"time",
		},
		{
			"sem object (status/join)",
			strings.Replace(exemploChirpStack, `"object": {`, `"nope_object": {`, 1),
			"object",
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			_, err := ParseUplinkChirpStack([]byte(c.raw))
			if err == nil {
				t.Fatal("esperava erro")
			}
			if !strings.Contains(err.Error(), c.motivo) {
				t.Errorf("erro = %q, esperado conter %q", err, c.motivo)
			}
		})
	}
}

func TestParseAutomatico_DetectaChirpStack(t *testing.T) {
	msg, err := ParseAutomatico([]byte(exemploChirpStack))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if _, ok := msg.(*UplinkChirpStack); !ok {
		t.Fatalf("esperado *UplinkChirpStack, veio %T", msg)
	}
}

func TestParseAutomatico_DetectaTTN(t *testing.T) {
	msg, err := ParseAutomatico([]byte(exemploTTN))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if _, ok := msg.(*Uplink); !ok {
		t.Fatalf("esperado *Uplink, veio %T", msg)
	}
}

func TestParseAutomatico_FormatoDesconhecido(t *testing.T) {
	_, err := ParseAutomatico([]byte(`{"foo":"bar"}`))
	if err == nil {
		t.Fatal("esperava erro para formato desconhecido")
	}
	if !strings.Contains(err.Error(), "formato desconhecido") {
		t.Errorf("erro = %q", err)
	}
}
