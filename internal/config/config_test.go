package config

import (
	"strings"
	"testing"
	"time"
)

func TestCarregarIngestor_Padroes(t *testing.T) {
	t.Setenv("MQTT_BROKER", "tcp://localhost:1883")
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")

	c, err := CarregarIngestor()
	if err != nil {
		t.Fatal(err)
	}
	if c.MQTTTopico != "v3/+/devices/+/up" || c.MQTTQoS != 1 || c.MongoDB != "tcc_bruto" {
		t.Errorf("padrões incorretos: %+v", c)
	}
}

func TestCarregarIngestor_ReportaTodosOsProblemas(t *testing.T) {
	t.Setenv("MQTT_BROKER", "")
	t.Setenv("MONGO_URI", "")
	t.Setenv("MQTT_QOS", "7")

	_, err := CarregarIngestor()
	if err == nil {
		t.Fatal("esperava erro")
	}
	for _, trecho := range []string{"MQTT_BROKER", "MONGO_URI", "MQTT_QOS"} {
		if !strings.Contains(err.Error(), trecho) {
			t.Errorf("erro %q deveria citar %s", err, trecho)
		}
	}
}

func TestCarregarProcessador(t *testing.T) {
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("WORKERS", "4")
	t.Setenv("RESERVA_SEGUNDOS", "30")
	t.Setenv("AUTO_CADASTRAR_SENSOR", "false")

	c, err := CarregarProcessador()
	if err != nil {
		t.Fatal(err)
	}
	if c.Workers != 4 || c.Reserva != 30*time.Second || c.AutoCadastrarSensor {
		t.Errorf("configuração incorreta: %+v", c)
	}
	if c.Intervalo != 500*time.Millisecond || c.MaxTentativas != 5 {
		t.Errorf("padrões incorretos: %+v", c)
	}
}

func TestCarregarProcessador_BooleanoInvalido(t *testing.T) {
	t.Setenv("MONGO_URI", "m")
	t.Setenv("DATABASE_URL", "p")
	t.Setenv("AUTO_CADASTRAR_SENSOR", "talvez")

	if _, err := CarregarProcessador(); err == nil {
		t.Error("esperava erro para booleano inválido")
	}
}

func TestCarregarAPI(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	c, err := CarregarAPI()
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" {
		t.Errorf("addr = %q", c.Addr)
	}
}
