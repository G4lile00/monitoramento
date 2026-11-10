// Package config carrega a configuração dos componentes a partir de variáveis
// de ambiente, mantendo credenciais fora do código-fonte (RNF06).
package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

// Ingestor configura o worker MQTT.
type Ingestor struct {
	MQTTBroker   string
	MQTTUsuario  string
	MQTTSenha    string
	MQTTTopico   string
	MQTTClientID string
	MQTTQoS      byte
	MongoURI     string
	MongoDB      string
}

// Processador configura o worker de processamento.
type Processador struct {
	MongoURI            string
	MongoDB             string
	DatabaseURL         string
	Workers             int
	MaxTentativas       int
	Reserva             time.Duration
	Intervalo           time.Duration
	AutoCadastrarSensor bool
}

// API configura a API REST.
type API struct {
	DatabaseURL string
	Addr        string
}

// CarregarIngestor lê a configuração do worker MQTT.
func CarregarIngestor() (Ingestor, error) {
	var l leitor
	c := Ingestor{
		MQTTBroker:   l.obrigatorio("MQTT_BROKER"),
		MQTTUsuario:  l.texto("MQTT_USUARIO", ""),
		MQTTSenha:    l.texto("MQTT_SENHA", ""),
		MQTTTopico:   l.texto("MQTT_TOPICO", "v3/+/devices/+/up"),
		MQTTClientID: l.texto("MQTT_CLIENT_ID", "tcc-ingestor"),
		MQTTQoS:      byte(l.inteiro("MQTT_QOS", 1, 0, 2)),
		MongoURI:     l.obrigatorio("MONGO_URI"),
		MongoDB:      l.texto("MONGO_DB", "tcc_bruto"),
	}
	return c, l.erro()
}

// CarregarProcessador lê a configuração do worker de processamento.
func CarregarProcessador() (Processador, error) {
	var l leitor
	c := Processador{
		MongoURI:            l.obrigatorio("MONGO_URI"),
		MongoDB:             l.texto("MONGO_DB", "tcc_bruto"),
		DatabaseURL:         l.obrigatorio("DATABASE_URL"),
		Workers:             l.inteiro("WORKERS", 2, 1, 256),
		MaxTentativas:       l.inteiro("MAX_TENTATIVAS", 5, 1, 1000),
		Reserva:             time.Duration(l.inteiro("RESERVA_SEGUNDOS", 60, 1, 86400)) * time.Second,
		Intervalo:           time.Duration(l.inteiro("INTERVALO_MS", 500, 10, 600000)) * time.Millisecond,
		AutoCadastrarSensor: l.booleano("AUTO_CADASTRAR_SENSOR", true),
	}
	return c, l.erro()
}

// CarregarAPI lê a configuração da API REST.
func CarregarAPI() (API, error) {
	var l leitor
	c := API{
		DatabaseURL: l.obrigatorio("DATABASE_URL"),
		Addr:        l.texto("API_ADDR", ":8080"),
	}
	return c, l.erro()
}

// leitor acumula todos os problemas de configuração para reportá-los juntos.
type leitor struct{ erros []string }

func (l *leitor) texto(chave, padrao string) string {
	if v := os.Getenv(chave); v != "" {
		return v
	}
	return padrao
}

func (l *leitor) obrigatorio(chave string) string {
	v := os.Getenv(chave)
	if v == "" {
		l.erros = append(l.erros, chave+" é obrigatória")
	}
	return v
}

func (l *leitor) inteiro(chave string, padrao, min, max int) int {
	v := os.Getenv(chave)
	if v == "" {
		return padrao
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min || n > max {
		l.erros = append(l.erros, chave+" deve ser um inteiro entre "+strconv.Itoa(min)+" e "+strconv.Itoa(max))
		return padrao
	}
	return n
}

func (l *leitor) booleano(chave string, padrao bool) bool {
	v := os.Getenv(chave)
	if v == "" {
		return padrao
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.erros = append(l.erros, chave+" deve ser true ou false")
		return padrao
	}
	return b
}

func (l *leitor) erro() error {
	if len(l.erros) == 0 {
		return nil
	}
	return errors.New(strings.Join(l.erros, "; "))
}
