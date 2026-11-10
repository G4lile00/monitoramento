// Package contrato define o contrato da mensagem de uplink do The Things
// Network (TTN) e as regras de validação e interpretação usadas tanto pelo
// worker MQTT (validação inicial) quanto pelo worker de processamento
// (extração das medições).
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

// Uplink representa o subconjunto do evento de uplink do TTN que a plataforma
// utiliza. Campos desconhecidos são ignorados; o documento original completo
// é preservado no MongoDB.
type Uplink struct {
	EndDeviceIDs  EndDeviceIDs   `json:"end_device_ids"`
	ReceivedAt    time.Time      `json:"received_at"`
	UplinkMessage *UplinkMessage `json:"uplink_message"`
}

// EndDeviceIDs identifica o dispositivo e a aplicação de origem.
type EndDeviceIDs struct {
	DeviceID       string `json:"device_id"`
	DevEUI         string `json:"dev_eui"`
	JoinEUI        string `json:"join_eui"`
	ApplicationIDs struct {
		ApplicationID string `json:"application_id"`
	} `json:"application_ids"`
}

// UplinkMessage contém os dados da mensagem recebida do dispositivo.
type UplinkMessage struct {
	FPort int `json:"f_port"`
	// FCnt é ponteiro porque o TTN omite o campo quando o contador é zero.
	FCnt           *uint32 `json:"f_cnt"`
	DecodedPayload struct {
		Message string `json:"message"`
	} `json:"decoded_payload"`
	Settings struct {
		Time *time.Time `json:"time"`
	} `json:"settings"`
}

// Medicao é uma grandeza ambiental extraída do texto decodificado.
type Medicao struct {
	Codigo  string
	Valor   float64
	Unidade string
}

// ParseUplink interpreta e valida um evento de uplink. A validação cobre
// apenas os campos necessários à identificação da mensagem (RF02); o conteúdo
// da medição só é interpretado por Medicoes, no worker de processamento.
// O DevEUI é normalizado para maiúsculas.
func ParseUplink(raw []byte) (*Uplink, error) {
	var u Uplink
	if err := json.Unmarshal(raw, &u); err != nil {
		return nil, fmt.Errorf("JSON inválido: %w", err)
	}

	ids := &u.EndDeviceIDs
	ids.DevEUI = strings.ToUpper(strings.TrimSpace(ids.DevEUI))

	switch {
	case len(ids.DevEUI) != 16 || !ehHex(ids.DevEUI):
		return nil, errors.New("end_device_ids.dev_eui ausente ou inválido")
	case ids.DeviceID == "":
		return nil, errors.New("end_device_ids.device_id ausente")
	case ids.ApplicationIDs.ApplicationID == "":
		return nil, errors.New("end_device_ids.application_ids.application_id ausente")
	case u.ReceivedAt.IsZero():
		return nil, errors.New("received_at ausente")
	case u.UplinkMessage == nil:
		return nil, errors.New("uplink_message ausente")
	}
	return &u, nil
}

// ChaveIdempotencia identifica unicamente um uplink para detectar reentregas
// MQTT (QoS 1): dev_eui|received_at|f_cnt. Quando o TTN omite f_cnt, o
// contador é tratado como zero.
func (u *Uplink) ChaveIdempotencia() string {
	contador := "0"
	if u.UplinkMessage != nil && u.UplinkMessage.FCnt != nil {
		contador = strconv.FormatUint(uint64(*u.UplinkMessage.FCnt), 10)
	}
	return u.EndDeviceIDs.DevEUI + "|" +
		u.ReceivedAt.UTC().Format(time.RFC3339Nano) + "|" + contador
}

// MedidoEm retorna o instante da medição: settings.time quando disponível;
// caso contrário, received_at.
func (u *Uplink) MedidoEm() time.Time {
	if u.UplinkMessage != nil {
		if t := u.UplinkMessage.Settings.Time; t != nil && !t.IsZero() {
			return t.UTC()
		}
	}
	return u.ReceivedAt.UTC()
}

// Medicoes extrai as grandezas de decoded_payload.message.
func (u *Uplink) Medicoes() ([]Medicao, error) {
	if u.UplinkMessage == nil {
		return nil, errors.New("uplink_message ausente")
	}
	return ParseMensagem(u.UplinkMessage.DecodedPayload.Message)
}

// ParseMensagem interpreta o texto CSV no formato
// "codigo,valor,unidade[,codigo,valor,unidade...]", por exemplo
// "temperatura,23.0,C,umidade,60.0,%".
func ParseMensagem(texto string) ([]Medicao, error) {
	texto = strings.TrimSpace(texto)
	if texto == "" {
		return nil, errors.New("decoded_payload.message vazio")
	}

	campos := strings.Split(texto, ",")
	if len(campos)%3 != 0 {
		return nil, fmt.Errorf("formato inesperado: %d campos (esperado múltiplo de 3)", len(campos))
	}

	medicoes := make([]Medicao, 0, len(campos)/3)
	for i := 0; i < len(campos); i += 3 {
		codigo := strings.ToLower(strings.TrimSpace(campos[i]))
		unidade := strings.TrimSpace(campos[i+2])
		if codigo == "" || unidade == "" {
			return nil, fmt.Errorf("grandeza ou unidade vazia na posição %d", i/3+1)
		}
		valor, err := strconv.ParseFloat(strings.TrimSpace(campos[i+1]), 64)
		if err != nil || math.IsNaN(valor) || math.IsInf(valor, 0) {
			return nil, fmt.Errorf("valor inválido para %q: %q", codigo, campos[i+1])
		}
		medicoes = append(medicoes, Medicao{Codigo: codigo, Valor: valor, Unidade: unidade})
	}
	return medicoes, nil
}

func ehHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
