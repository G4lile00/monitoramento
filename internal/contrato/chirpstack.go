package contrato

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// UplinkChirpStack representa o subconjunto do evento de uplink do ChirpStack
// v4 utilizado pela plataforma. Campos desconhecidos são ignorados; o
// documento original completo é preservado no MongoDB.
//
// Diferenças em relação ao TTN v3:
//   - deviceInfo concentra tenant/aplicação/perfil/dispositivo (e tags).
//   - o campo "object" já traz as medições decodificadas como pares
//     chave/valor (não como texto CSV como no decoded_payload.message do TTN).
//   - "deduplicationId" é um UUID gerado pelo ChirpStack, ideal como chave
//     de idempotência.
//   - "time" é o instante do evento no servidor (RFC 3339 com offset).
//
// Eventos do ChirpStack que não são uplinks (logs, joins, status) não trazem
// "object" nem "data": são rejeitados por ParseUplinkChirpStack.
type UplinkChirpStack struct {
	DeduplicationID string      `json:"deduplicationId"`
	Time            time.Time   `json:"time"`
	DeviceInfo      DeviceInfo  `json:"deviceInfo"`
	DevAddr         string      `json:"devAddr"`
	FCnt            *uint32     `json:"fCnt"` // ponteiro: pode ser zero ou ausente
	FPort           *int        `json:"fPort"`
	Data            string      `json:"data"`
	Object          map[string]json.Number `json:"object"`
	RxInfo          []json.RawMessage `json:"rxInfo"` // preservado, não interpretado
}

// DeviceInfo identifica o dispositivo, a aplicação e (opcionalmente) a
// localização cadastrada como tags no ChirpStack.
type DeviceInfo struct {
	TenantID          string `json:"tenantId"`
	TenantName        string `json:"tenantName"`
	ApplicationID     string `json:"applicationId"`
	ApplicationName   string `json:"applicationName"`
	DeviceProfileID   string `json:"deviceProfileId"`
	DeviceProfileName string `json:"deviceProfileName"`
	DeviceName        string `json:"deviceName"`
	DevEUI            string `json:"devEui"`
	DeviceClassEnabled string `json:"deviceClassEnabled"`
	Tags              Tags   `json:"tags"`
}

// Tags carrega as tags cadastradas no ChirpStack. As três usadas pela
// plataforma são Latitude, Longitude e Altitude (strings no JSON do
// ChirpStack); demais tags são ignoradas.
type Tags struct {
	Latitude  string `json:"Latitude"`
	Longitude string `json:"Longitude"`
	Altitude  string `json:"Altitude"`
}

// ParseUplinkChirpStack interpreta e valida um evento de uplink do
// ChirpStack v4. A validação cobre apenas os campos necessários à
// identificação (deduplicationId, deviceInfo.devEui, deviceInfo.applicationId
// e time) e à existência de medições (object). DevEUI é normalizado para
// maiúsculas.
func ParseUplinkChirpStack(raw []byte) (*UplinkChirpStack, error) {
	var u UplinkChirpStack
	if err := json.Unmarshal(raw, &u); err != nil {
		return nil, fmt.Errorf("JSON inválido: %w", err)
	}

	u.DeviceInfo.DevEUI = strings.ToUpper(strings.TrimSpace(u.DeviceInfo.DevEUI))
	u.DeduplicationID = strings.TrimSpace(u.DeduplicationID)

	switch {
	case u.DeduplicationID == "":
		return nil, errors.New("deduplicationId ausente (evento sem uplink?)")
	case len(u.DeviceInfo.DevEUI) != 16 || !ehHex(u.DeviceInfo.DevEUI):
		return nil, errors.New("deviceInfo.devEui ausente ou inválido")
	case u.DeviceInfo.ApplicationID == "":
		return nil, errors.New("deviceInfo.applicationId ausente")
	case u.Time.IsZero():
		return nil, errors.New("time ausente")
	case len(u.Object) == 0:
		// Sem object: trata-se de status/join/log, não de uplink de medição.
		return nil, errors.New("object ausente (não é um uplink de medição)")
	}
	return &u, nil
}

// ChaveIdempotencia identifica unicamente um uplink para detectar reentregas
// MQTT (QoS 1). O ChirpStack já fornece o deduplicationId (UUID), que é
// suficiente e mais robusto que recompor a chave a partir dos campos. O
// prefixo "cs:" distingue do espaço de chaves do TTN (que começa com DevEUI).
func (u *UplinkChirpStack) ChaveIdempotencia() string {
	return "cs:" + u.DeduplicationID
}

// MedidoEm retorna o instante da medição. O ChirpStack entrega "time" já em
// UTC (com offset +00:00 no formato observado); usamos direto.
func (u *UplinkChirpStack) MedidoEm() time.Time { return u.Time.UTC() }

// Medicoes extrai as grandezas de "object". Cada par chave/valor vira uma
// Medicao com código normalizado e unidade conhecida. Campos desconhecidos
// (fora do catálogo abaixo) são ignorados, em vez de derrubar o documento:
// isso permite que um mesmo dispositivo publique grandezas adicionais sem
// quebrar o processamento.
//
// Mapeamento (campo ChirpStack → codigo/unidade no PostgreSQL):
//
//      vibracao_pico_ms2    → vibracao    / m/s²
//      temperatura_c        → temperatura / C     (já existente)
//      bateria_v            → bateria    / V
//      umidade_pct          → umidade    / %      (já existente)
//      pressao_hpa          → pressao    / hPa
//      inclinacao_sensor_deg → inclinacao / °
//      solo_pct             → solo       / %
func (u *UplinkChirpStack) Medicoes() ([]Medicao, error) {
	medicoes := make([]Medicao, 0, len(u.Object))
	for campo, raw := range u.Object {
		codigo, unidade, ok := mapearGrandeza(campo)
		if !ok {
			continue
		}
		valor, err := raw.Float64()
		if err != nil || math.IsNaN(valor) || math.IsInf(valor, 0) {
			return nil, fmt.Errorf("valor inválido para %q: %q", campo, raw.String())
		}
		medicoes = append(medicoes, Medicao{Codigo: codigo, Valor: valor, Unidade: unidade})
	}
	if len(medicoes) == 0 {
		return nil, errors.New("object não contém grandezas conhecidas")
	}
	return medicoes, nil
}

// Localizacao devolve latitude/longitude/altitude quando as tags do
// ChirpStack estiverem presentes. São usadas para preencher
// sensor.localizacao na primeira vez que o sensor é visto (ou quando o
// cadastro ainda não tem localização).
func (u *UplinkChirpStack) Localizacao() (lat, lon, alt *float64) {
	latStr := strings.TrimSpace(u.DeviceInfo.Tags.Latitude)
	lonStr := strings.TrimSpace(u.DeviceInfo.Tags.Longitude)
	altStr := strings.TrimSpace(u.DeviceInfo.Tags.Altitude)
	if latStr == "" || lonStr == "" {
		return nil, nil, nil
	}
	latF, err := strconv.ParseFloat(latStr, 64)
	if err != nil || math.IsNaN(latF) || math.IsInf(latF, 0) {
		return nil, nil, nil
	}
	lonF, err := strconv.ParseFloat(lonStr, 64)
	if err != nil || math.IsNaN(lonF) || math.IsInf(lonF, 0) {
		return nil, nil, nil
	}
	lat = &latF
	lon = &lonF
	if altStr != "" {
		// Altitude vem como "6.8 metros " no ChirpStack; pegamos só a parte
		// numérica inicial.
		i := 0
		for i < len(altStr) {
			c := altStr[i]
			if (c < '0' || c > '9') && c != '.' && c != '-' && c != '+' {
				break
			}
			i++
		}
		if i > 0 {
			if v, err := strconv.ParseFloat(altStr[:i], 64); err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) {
				alt = &v
			}
		}
	}
	return lat, lon, alt
}

// ---- Métodos de Mensagem (interface comum entre contratos) ----

// ApplicationID retorna o deviceInfo.applicationId.
func (u *UplinkChirpStack) ApplicationID() string { return u.DeviceInfo.ApplicationID }

// DeviceID retorna o deviceInfo.deviceName (equivalente funcional do
// device_id do TTN). Quando vazio, cai para DevEUI.
func (u *UplinkChirpStack) DeviceID() string {
	if u.DeviceInfo.DeviceName != "" {
		return u.DeviceInfo.DeviceName
	}
	return u.DeviceInfo.DevEUI
}

// DevEUI retorna o dev_eui normalizado.
func (u *UplinkChirpStack) DevEUI() string { return u.DeviceInfo.DevEUI }

// JoinEUI retorna vazio: o ChirpStack v4 não inclui JoinEUI no deviceInfo
// do evento de uplink (aparece em "context" de eventos OTAA, que já são
// rejeitados por não terem "object").
func (u *UplinkChirpStack) JoinEUI() string { return "" }

// mapearGrandeza converte o nome do campo do "object" do ChirpStack para o
// par (codigo, unidade) usado no catálogo do PostgreSQL. Retorna ok=false
// para campos desconhecidos (ignorados em Medicoes).
func mapearGrandeza(campo string) (codigo, unidade string, ok bool) {
	switch campo {
	case "vibracao_pico_ms2":
		return "vibracao", "m/s²", true
	case "temperatura_c":
		return "temperatura", "C", true
	case "bateria_v":
		return "bateria", "V", true
	case "umidade_pct":
		return "umidade", "%", true
	case "pressao_hpa":
		return "pressao", "hPa", true
	case "inclinacao_sensor_deg":
		return "inclinacao", "°", true
	case "solo_pct":
		return "solo", "%", true
	}
	return "", "", false
}
