package contrato

import (
	"strings"
	"testing"
	"time"
)

// Exemplo da Listagem do contrato de mensagem (main.tex).
const exemploTTN = `{
  "end_device_ids": {
    "device_id": "e31hltec",
    "application_ids": {"application_id": "e31"},
    "dev_eui": "70B3D57ED00724FA"
  },
  "received_at": "2025-08-21T21:08:51.011075263Z",
  "uplink_message": {
    "f_port": 1,
    "f_cnt": 18,
    "decoded_payload": {"message": "temperatura,23.0,C,umidade,60.0,%"},
    "settings": {"time": "2025-08-21T21:08:50.613867Z"}
  }
}`

func TestParseUplink_Exemplo(t *testing.T) {
	u, err := ParseUplink([]byte(exemploTTN))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if u.EndDeviceIDs.DeviceID != "e31hltec" || u.EndDeviceIDs.ApplicationIDs.ApplicationID != "e31" {
		t.Errorf("identificação incorreta: %+v", u.EndDeviceIDs)
	}

	want := "70B3D57ED00724FA|2025-08-21T21:08:51.011075263Z|18"
	if got := u.ChaveIdempotencia(); got != want {
		t.Errorf("chave = %q, quer %q", got, want)
	}

	wantT := time.Date(2025, 8, 21, 21, 8, 50, 613867000, time.UTC)
	if got := u.MedidoEm(); !got.Equal(wantT) {
		t.Errorf("MedidoEm = %v, quer %v", got, wantT)
	}

	m, err := u.Medicoes()
	if err != nil {
		t.Fatalf("Medicoes: %v", err)
	}
	if len(m) != 2 || m[0] != (Medicao{"temperatura", 23.0, "C"}) || m[1] != (Medicao{"umidade", 60.0, "%"}) {
		t.Errorf("medições incorretas: %+v", m)
	}
}

func TestChave_ReentregaGeraMesmaChave(t *testing.T) {
	a, _ := ParseUplink([]byte(exemploTTN))
	b, _ := ParseUplink([]byte(exemploTTN))
	if a.ChaveIdempotencia() != b.ChaveIdempotencia() {
		t.Error("a mesma mensagem deve gerar a mesma chave")
	}
}

func TestChave_SemFCntUsaZero(t *testing.T) {
	raw := strings.Replace(exemploTTN, `"f_cnt": 18,`, "", 1)
	u, err := ParseUplink([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(u.ChaveIdempotencia(), "|0") {
		t.Errorf("chave = %q, esperado sufixo |0", u.ChaveIdempotencia())
	}
}

func TestMedidoEm_SemSettingsUsaReceivedAt(t *testing.T) {
	raw := strings.Replace(exemploTTN, `"settings": {"time": "2025-08-21T21:08:50.613867Z"}`, `"settings": {}`, 1)
	u, err := ParseUplink([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2025, 8, 21, 21, 8, 51, 11075263, time.UTC)
	if got := u.MedidoEm(); !got.Equal(want) {
		t.Errorf("MedidoEm = %v, quer %v", got, want)
	}
}

func TestParseUplink_DevEUINormalizado(t *testing.T) {
	raw := strings.Replace(exemploTTN, "70B3D57ED00724FA", "70b3d57ed00724fa", 1)
	u, err := ParseUplink([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if u.EndDeviceIDs.DevEUI != "70B3D57ED00724FA" {
		t.Errorf("dev_eui = %q", u.EndDeviceIDs.DevEUI)
	}
}

func TestParseUplink_Rejeicoes(t *testing.T) {
	casos := []struct {
		nome, raw, motivo string
	}{
		{"não é JSON", `não sou json`, "JSON inválido"},
		{"array", `[]`, "JSON inválido"},
		{"vazio", `{}`, "dev_eui"},
		{"dev_eui curto", strings.Replace(exemploTTN, "70B3D57ED00724FA", "70B3", 1), "dev_eui"},
		{"dev_eui não hexadecimal", strings.Replace(exemploTTN, "70B3D57ED00724FA", "ZZB3D57ED00724FA", 1), "dev_eui"},
		{"sem device_id", strings.Replace(exemploTTN, `"device_id": "e31hltec",`, "", 1), "device_id"},
		{"sem application_id", strings.Replace(exemploTTN, `"application_ids": {"application_id": "e31"},`, "", 1), "application_id"},
		{"sem received_at", strings.Replace(exemploTTN, `"received_at": "2025-08-21T21:08:51.011075263Z",`, "", 1), "received_at"},
		{"received_at malformado", strings.Replace(exemploTTN, "2025-08-21T21:08:51.011075263Z", "ontem", 1), "JSON inválido"},
		{"sem uplink_message", `{"end_device_ids":{"device_id":"d","dev_eui":"70B3D57ED00724FA","application_ids":{"application_id":"a"}},"received_at":"2025-08-21T21:08:51Z"}`, "uplink_message"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			_, err := ParseUplink([]byte(c.raw))
			if err == nil {
				t.Fatal("esperava erro")
			}
			if !strings.Contains(err.Error(), c.motivo) {
				t.Errorf("erro = %q, esperado conter %q", err, c.motivo)
			}
		})
	}
}

func TestParseMensagem(t *testing.T) {
	t.Run("espaços e maiúsculas", func(t *testing.T) {
		m, err := ParseMensagem(" Temperatura , 23.5 , C ")
		if err != nil {
			t.Fatal(err)
		}
		if len(m) != 1 || m[0] != (Medicao{"temperatura", 23.5, "C"}) {
			t.Errorf("medições = %+v", m)
		}
	})

	invalidas := map[string]string{
		"vazia":                 "",
		"campos faltando":       "temperatura,23.0",
		"valor não numérico":    "temperatura,abc,C",
		"NaN":                   "temperatura,NaN,C",
		"infinito":              "temperatura,Inf,C",
		"grandeza vazia":        ",23.0,C",
		"unidade vazia":         "temperatura,23.0,",
		"segunda grandeza ruim": "temperatura,23.0,C,umidade,x,%",
	}
	for nome, texto := range invalidas {
		t.Run(nome, func(t *testing.T) {
			if _, err := ParseMensagem(texto); err == nil {
				t.Errorf("esperava erro para %q", texto)
			}
		})
	}
}
