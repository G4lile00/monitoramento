// Package modelo reúne os tipos de dados compartilhados entre a camada de
// consulta (API) e o acesso ao PostgreSQL.
package modelo

import "time"

// Sensor é um dispositivo cadastrado no PostgreSQL.
type Sensor struct {
	ID                int        `json:"id_sensor"`
	CodigoDispositivo string     `json:"codigo_dispositivo"`
	DevEUI            string     `json:"dev_eui"`
	Nome              *string    `json:"nome"`
	Ativo             bool       `json:"ativo"`
	UltimoUplinkEm    *time.Time `json:"ultimo_uplink_em"`
	Latitude          *float64   `json:"latitude"`
	Longitude         *float64   `json:"longitude"`
}

// Leitura é uma medição estruturada (uma grandeza de um sensor em um instante).
type Leitura struct {
	DevEUI   string    `json:"dev_eui"`
	Grandeza string    `json:"grandeza"`
	Unidade  string    `json:"unidade"`
	MedidoEm time.Time `json:"medido_em"`
	Valor    float64   `json:"valor"`
	// MongoID referencia logicamente o documento bruto em mensagens_brutas.
	MongoID string `json:"mongo_id"`
}

// FiltroLeituras define os parâmetros de consulta de leituras (RF09).
// O intervalo é semiaberto: De <= medido_em < Ate.
type FiltroLeituras struct {
	DevEUI   *string
	Grandeza *string
	De       time.Time
	Ate      time.Time
	Limite   int
}
