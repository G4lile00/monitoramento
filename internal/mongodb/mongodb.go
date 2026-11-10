// Package mongodb implementa a camada bruta (mensagens_brutas e
// mensagens_rejeitadas) e a fila de trabalho do worker sobre o MongoDB.
//
// Store satisfaz ingestao.Armazenamento e processamento.Fila.
package mongodb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"tcc-monitoramento/internal/processamento"
)

// Estados do documento na fila de trabalho.
const (
	StatusPendente    = "pendente"
	StatusProcessando = "processando"
	StatusProcessado  = "processado"
	StatusErro        = "erro"
)

// ErrReservaPerdida indica que o documento não está mais reservado pelo worker
// (a reserva expirou e outro worker o assumiu). É inofensivo: a gravação no
// PostgreSQL é idempotente.
var ErrReservaPerdida = errors.New("reserva perdida ou documento inexistente")

// Store acessa as coleções do MongoDB.
type Store struct {
	client     *mongo.Client
	brutas     *mongo.Collection
	rejeitadas *mongo.Collection
}

type documentoBruto struct {
	ID                bson.ObjectID `bson:"_id,omitempty"`
	ChaveIdempotencia string        `bson:"chave_idempotencia"`
	TopicoMQTT        string        `bson:"topico_mqtt"`
	RecebidaEm        time.Time     `bson:"recebida_em"`
	Payload           bson.Raw      `bson:"payload"`
	Status            string        `bson:"status"`
	Tentativas        int           `bson:"tentativas"`
	ReservadoPor      string        `bson:"reservado_por,omitempty"`
	ReservadoAte      *time.Time    `bson:"reservado_ate,omitempty"`
	ProcessadoEm      *time.Time    `bson:"processado_em,omitempty"`
	UltimoErro        string        `bson:"ultimo_erro,omitempty"`
}

type documentoRejeitado struct {
	RecebidaEm time.Time `bson:"recebida_em"`
	TopicoMQTT string    `bson:"topico_mqtt"`
	Conteudo   string    `bson:"conteudo"`
	Motivo     string    `bson:"motivo"`
}

// Conectar abre a conexão e verifica que o servidor responde.
func Conectar(ctx context.Context, uri, banco string) (*Store, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("conectar ao MongoDB: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("MongoDB não respondeu: %w", err)
	}
	db := client.Database(banco)
	return &Store{
		client:     client,
		brutas:     db.Collection("mensagens_brutas"),
		rejeitadas: db.Collection("mensagens_rejeitadas"),
	}, nil
}

// Fechar encerra a conexão.
func (s *Store) Fechar(ctx context.Context) error { return s.client.Disconnect(ctx) }

// GarantirIndices cria os índices do modelo (idempotente).
func (s *Store) GarantirIndices(ctx context.Context) error {
	_, err := s.brutas.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "chave_idempotencia", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("ux_chave_idempotencia"),
		},
		{
			Keys:    bson.D{{Key: "status", Value: 1}, {Key: "recebida_em", Value: 1}},
			Options: options.Index().SetName("ix_fila_worker"),
		},
		{
			Keys:    bson.D{{Key: "recebida_em", Value: 1}},
			Options: options.Index().SetName("ix_recebida_em"),
		},
	})
	if err != nil {
		return fmt.Errorf("criar índices: %w", err)
	}
	return nil
}

// InserirBruta implementa ingestao.Armazenamento.
func (s *Store) InserirBruta(ctx context.Context, chave, topico string, recebidaEm time.Time, json []byte) (bool, error) {
	var doc bson.D
	if err := bson.UnmarshalExtJSON(json, false, &doc); err != nil {
		return false, fmt.Errorf("converter payload para BSON: %w", err)
	}
	payload, err := bson.Marshal(doc)
	if err != nil {
		return false, fmt.Errorf("serializar payload: %w", err)
	}

	_, err = s.brutas.InsertOne(ctx, documentoBruto{
		ChaveIdempotencia: chave,
		TopicoMQTT:        topico,
		RecebidaEm:        recebidaEm.UTC(),
		Payload:           payload,
		Status:            StatusPendente,
	})
	if mongo.IsDuplicateKeyError(err) {
		return true, nil
	}
	return false, err
}

// InserirRejeitada implementa ingestao.Armazenamento.
func (s *Store) InserirRejeitada(ctx context.Context, topico string, recebidaEm time.Time, conteudo []byte, motivo string) error {
	_, err := s.rejeitadas.InsertOne(ctx, documentoRejeitado{
		RecebidaEm: recebidaEm.UTC(),
		TopicoMQTT: topico,
		Conteudo:   string(conteudo),
		Motivo:     motivo,
	})
	return err
}

// Reservar implementa processamento.Fila. A busca e a reserva acontecem em uma
// única operação atômica, então dois workers nunca recebem o mesmo documento.
func (s *Store) Reservar(ctx context.Context, workerID string, validade time.Duration) (*processamento.Item, error) {
	agora := time.Now().UTC()

	filtro := bson.M{"$or": bson.A{
		bson.M{"status": StatusPendente},
		bson.M{"status": StatusProcessando, "reservado_ate": bson.M{"$lt": agora}},
	}}
	atualizacao := bson.M{
		"$set": bson.M{
			"status":        StatusProcessando,
			"reservado_por": workerID,
			"reservado_ate": agora.Add(validade),
		},
		"$inc": bson.M{"tentativas": 1},
	}
	opts := options.FindOneAndUpdate().
		SetSort(bson.D{{Key: "recebida_em", Value: 1}}).
		SetReturnDocument(options.After)

	var doc documentoBruto
	err := s.brutas.FindOneAndUpdate(ctx, filtro, atualizacao, opts).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reservar documento: %w", err)
	}

	json, err := bson.MarshalExtJSON(doc.Payload, false, false)
	if err != nil {
		return nil, fmt.Errorf("converter payload para JSON: %w", err)
	}
	return &processamento.Item{ID: doc.ID.Hex(), Tentativas: doc.Tentativas, JSON: json}, nil
}

// Concluir implementa processamento.Fila.
func (s *Store) Concluir(ctx context.Context, id, workerID string) error {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("id inválido %q: %w", id, err)
	}
	res, err := s.brutas.UpdateOne(ctx,
		bson.M{"_id": oid, "status": StatusProcessando, "reservado_por": workerID},
		bson.M{
			"$set":   bson.M{"status": StatusProcessado, "processado_em": time.Now().UTC()},
			"$unset": bson.M{"ultimo_erro": ""},
		})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrReservaPerdida
	}
	return nil
}

// Falhar implementa processamento.Fila.
func (s *Store) Falhar(ctx context.Context, id, workerID, motivo string, definitivo bool) error {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("id inválido %q: %w", id, err)
	}
	status := StatusPendente
	if definitivo {
		status = StatusErro
	}
	res, err := s.brutas.UpdateOne(ctx,
		bson.M{"_id": oid, "status": StatusProcessando, "reservado_por": workerID},
		bson.M{
			"$set":   bson.M{"status": status, "ultimo_erro": motivo},
			"$unset": bson.M{"reservado_por": "", "reservado_ate": ""},
		})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrReservaPerdida
	}
	return nil
}
