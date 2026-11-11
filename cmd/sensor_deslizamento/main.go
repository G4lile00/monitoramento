// Comando sensor_deslizamento assina o tópico MQTT do ChirpStack v4 e grava
// cada uplink no MongoDB (camada bruta), usando o mesmo ingestao.Servico do
// comando ingestor (TTN). A diferença está apenas nas variáveis de ambiente
// usadas (prefixo SD_MQTT_*) e no tópico default, que segue o formato do
// ChirpStack v4: application/+/device/+/event/up.
//
// Como o ingestao.Servico usa contrato.ParseAutomatico, o mesmo documento
// bruto em mensagens_brutas é consumido depois pelo processador sem
// modificações: o parser detecta o formato (TTN vs ChirpStack) pelo conteúdo.
//
// A confirmação (ACK) ao broker só é enviada depois que a mensagem foi
// persistida (ou registrada como rejeitada). Com QoS 1 e sessão persistente,
// uma mensagem não confirmada é reentregue; a chave de idempotência
// ("cs:" + deduplicationId) evita duplicatas no MongoDB.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	time "time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"monitoramento/internal/config"
	"monitoramento/internal/ingestao"
	"monitoramento/internal/mongodb"
)

const (
	tentativasPersistencia = 5
	esperaInicial          = 500 * time.Millisecond
)

func main() {
	if err := run(); err != nil {
		slog.Error("falha no sensor_deslizamento", "erro", err)
		os.Exit(1)
	}
}

func run() error {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := config.CarregarIngestorSD()
	if err != nil {
		return fmt.Errorf("configuração inválida: %w", err)
	}

	sinal, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer parar()

	store, err := mongodb.Conectar(sinal, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		return err
	}
	defer store.Fechar(context.Background())
	if err := store.GarantirIndices(sinal); err != nil {
		return err
	}

	svc := ingestao.Novo(store, log)

	// Contexto do trabalho em andamento: só é cancelado depois do
	// desligamento do cliente MQTT, para as mensagens em curso terminarem.
	trabalho, cancelarTrabalho := context.WithCancel(context.Background())
	defer cancelarTrabalho()

	var emAndamento sync.WaitGroup
	tratar := func(_ mqtt.Client, m mqtt.Message) {
		emAndamento.Add(1)
		defer emAndamento.Done()
		err := svc.TratarComRetentativas(trabalho, m.Topic(), m.Payload(), tentativasPersistencia, esperaInicial)
		if err != nil {
			// Sem ACK: o broker reentrega quando a sessão for retomada.
			log.Error("mensagem não persistida; sem confirmação ao broker", "topico", m.Topic(), "erro", err)
			return
		}
		m.Ack()
	}

	opts := mqtt.NewClientOptions().
		AddBroker(cfg.MQTTBroker).
		SetClientID(cfg.MQTTClientID).
		SetCleanSession(false). // sessão persistente: o broker guarda o que chegar offline
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetKeepAlive(30 * time.Second).
		SetOrderMatters(false).
		SetAutoAckDisabled(true)

	if cfg.MQTTUsuario != "" {
		opts.SetUsername(cfg.MQTTUsuario).SetPassword(cfg.MQTTSenha)
	}
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		log.Warn("conexão MQTT perdida; reconectando", "erro", err)
	})
	// Assinar no OnConnect refaz a assinatura a cada reconexão.
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		log.Info("conectado ao broker MQTT", "broker", cfg.MQTTBroker, "topico", cfg.MQTTTopico)
		t := c.Subscribe(cfg.MQTTTopico, cfg.MQTTQoS, tratar)
		go func() {
			t.Wait()
			if err := t.Error(); err != nil {
				log.Error("falha ao assinar o tópico", "topico", cfg.MQTTTopico, "erro", err)
			}
		}()
	})

	cliente := mqtt.NewClient(opts)
	conexao := cliente.Connect()
	if !conexao.WaitTimeout(60 * time.Second) {
		return errors.New("tempo esgotado ao conectar ao broker MQTT")
	}
	if err := conexao.Error(); err != nil {
		return fmt.Errorf("conectar ao broker MQTT: %w", err)
	}

	<-sinal.Done()
	log.Info("encerrando")
	cliente.Disconnect(5000) // aguarda até 5 s as mensagens em curso
	emAndamento.Wait()
	cancelarTrabalho()
	return nil
}
