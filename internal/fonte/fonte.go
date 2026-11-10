// Package fonte lê mensagens de uplink de um arquivo JSON, como alternativa
// ao broker MQTT para testes locais. O resultado alimenta o mesmo
// ingestao.Servico que o worker MQTT usará.
package fonte

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Ler extrai as mensagens de um arquivo JSON em um dos formatos:
//   - um array de objetos: [ {...}, {...} ]
//   - um único objeto: { ... }
//   - vários objetos em sequência (um por linha, "NDJSON")
//
// Cada mensagem é devolvida exatamente como está escrita no arquivo.
// Elementos semanticamente inválidos (por exemplo, sem dev_eui) NÃO causam
// erro aqui: seguem adiante para que a validação da ingestão os rejeite,
// exercitando o mesmo caminho do MQTT.
func Ler(r io.Reader) ([][]byte, error) {
	dados, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("ler arquivo: %w", err)
	}
	dados = bytes.TrimSpace(dados)
	if len(dados) == 0 {
		return nil, errors.New("arquivo vazio")
	}

	if dados[0] == '[' {
		var itens []json.RawMessage
		if err := json.Unmarshal(dados, &itens); err != nil {
			return nil, fmt.Errorf("array JSON inválido: %w", err)
		}
		saida := make([][]byte, len(itens))
		for i, it := range itens {
			saida[i] = []byte(it)
		}
		return saida, nil
	}

	var saida [][]byte
	dec := json.NewDecoder(bytes.NewReader(dados))
	for {
		var raw json.RawMessage
		err := dec.Decode(&raw)
		if errors.Is(err, io.EOF) {
			return saida, nil
		}
		if err != nil {
			return nil, fmt.Errorf("mensagem %d: %w", len(saida)+1, err)
		}
		saida = append(saida, []byte(raw))
	}
}
