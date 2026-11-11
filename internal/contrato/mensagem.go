package contrato

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Mensagem é a abstração comum entre os contratos suportados (atualmente
// uplink TTN v3 e uplink ChirpStack v4). Tanto o worker de ingestão quanto o
// worker de processamento operam sobre Mensagem, sem conhecer o formato
// específico recebido.
//
// Os métodos cobrem:
//   - identificação do dispositivo (usada para resolver/cadastrar o sensor);
//   - chave de idempotência (anti-reentrega MQTT QoS 1);
//   - instante da medição (coluna temporal da hipertabela);
//   - grandezas extraídas do payload decodificado;
//   - localização opcional vinda junto com a mensagem (tags do ChirpStack).
type Mensagem interface {
	ChaveIdempotencia() string
	MedidoEm() time.Time

	ApplicationID() string
	DeviceID() string
	DevEUI() string
	JoinEUI() string

	Medicoes() ([]Medicao, error)
	Localizacao() (lat, lon, alt *float64)
}

// ParseAutomatico identifica o formato do payload e chama o parser
// correspondente. Suporta hoje:
//
//   - TTN v3: presença do campo "uplink_message" (ou "end_device_ids")
//   - ChirpStack v4: presença do campo "deviceInfo" (com "devEui")
//
// Quando nenhum dos dois é reconhecido, retorna erro; a mensagem segue para
// mensagens_rejeitadas como nas demais falhas de validação (RF04).
func ParseAutomatico(raw []byte) (Mensagem, error) {
	var topo map[string]json.RawMessage
	if err := json.Unmarshal(raw, &topo); err != nil {
		return nil, fmt.Errorf("JSON inválido: %w", err)
	}
	if _, ok := topo["uplink_message"]; ok {
		u, err := ParseUplink(raw)
		if err != nil {
			return nil, err
		}
		return u, nil
	}
	if _, ok := topo["deviceInfo"]; ok {
		u, err := ParseUplinkChirpStack(raw)
		if err != nil {
			return nil, err
		}
		return u, nil
	}
	return nil, errors.New("formato desconhecido: nem uplink_message (TTN) nem deviceInfo (ChirpStack)")
}
