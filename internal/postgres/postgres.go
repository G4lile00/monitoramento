// Package postgres implementa o acesso ao PostgreSQL (TimescaleDB + PostGIS):
// gravação idempotente das leituras pelo worker e consultas da API.
//
// Repo satisfaz processamento.Repositorio e api.Consultas.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"monitoramento/internal/modelo"
	"monitoramento/internal/processamento"
)

// Repo acessa o PostgreSQL.
type Repo struct {
	pool *pgxpool.Pool
	// autoCadastrar cria aplicação e sensor desconhecidos ao processar a
	// primeira mensagem (útil enquanto as rotas administrativas não existem).
	autoCadastrar bool
}

// Conectar abre o pool e verifica que o banco responde.
func Conectar(ctx context.Context, url string, autoCadastrar bool) (*Repo, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("criar pool PostgreSQL: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("PostgreSQL não respondeu: %w", err)
	}
	return &Repo{pool: pool, autoCadastrar: autoCadastrar}, nil
}

// Fechar libera o pool.
func (r *Repo) Fechar() { r.pool.Close() }

// Pingar implementa api.Consultas.
func (r *Repo) Pingar(ctx context.Context) error { return r.pool.Ping(ctx) }

// GravarLeituras implementa processamento.Repositorio. Tudo ocorre em uma
// transação; a chave primária (mongo_id, id_grandeza, medido_em) com
// ON CONFLICT DO NOTHING torna a regravação segura (RNF02).
func (r *Repo) GravarLeituras(ctx context.Context, l processamento.Lote) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("iniciar transação: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // após Commit retorna ErrTxClosed, ignorável

	idSensor, err := r.resolverSensor(ctx, tx, l)
	if err != nil {
		return err
	}

	for _, m := range l.Medicoes {
		var idGrandeza int16
		err := tx.QueryRow(ctx, `SELECT id_grandeza FROM grandeza WHERE codigo = $1`, m.Codigo).Scan(&idGrandeza)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: grandeza %q não cadastrada", processamento.ErrPermanente, m.Codigo)
		}
		if err != nil {
			return fmt.Errorf("buscar grandeza: %w", err)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO leitura (id_sensor, id_grandeza, medido_em, valor, mongo_id)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (mongo_id, id_grandeza, medido_em) DO NOTHING`,
			idSensor, idGrandeza, l.MedidoEm, m.Valor, l.MongoID)
		if err != nil {
			return fmt.Errorf("inserir leitura: %w", err)
		}
	}

	_, err = tx.Exec(ctx, `
		UPDATE sensor
		   SET ultimo_uplink_em = GREATEST(COALESCE(ultimo_uplink_em, $2), $2)
		 WHERE id_sensor = $1`, idSensor, l.MedidoEm)
	if err != nil {
		return fmt.Errorf("atualizar último uplink: %w", err)
	}

	// Localização trazida pela mensagem (ex.: tags do ChirpStack). Só
	// preenche quando o sensor ainda não tem localização cadastrada — não
	// sobrescreve um cadastro manual feito por rotas administrativas.
	if l.Latitude != nil && l.Longitude != nil {
		altArg := any(nil)
		if l.Altitude != nil {
			altArg = *l.Altitude
		}
		_, err = tx.Exec(ctx, `
			UPDATE sensor
			   SET localizacao = ST_SetSRID(ST_MakePoint($2, $3), 4326),
			       altitude_m  = COALESCE(altitude_m, $4)
			 WHERE id_sensor = $1 AND localizacao IS NULL`,
			idSensor, *l.Longitude, *l.Latitude, altArg)
		if err != nil {
			return fmt.Errorf("atualizar localização: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("confirmar transação: %w", err)
	}
	return nil
}

func (r *Repo) resolverSensor(ctx context.Context, tx pgx.Tx, l processamento.Lote) (int, error) {
	id, err := buscarSensor(ctx, tx, l.DevEUI)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("buscar sensor: %w", err)
	}
	if !r.autoCadastrar {
		return 0, fmt.Errorf("%w: sensor %s não cadastrado", processamento.ErrPermanente, l.DevEUI)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO aplicacao (codigo_aplicacao) VALUES ($1) ON CONFLICT (codigo_aplicacao) DO NOTHING`,
		l.ApplicationID); err != nil {
		return 0, fmt.Errorf("cadastrar aplicação: %w", err)
	}
	var idApp int
	if err := tx.QueryRow(ctx,
		`SELECT id_aplicacao FROM aplicacao WHERE codigo_aplicacao = $1`,
		l.ApplicationID).Scan(&idApp); err != nil {
		return 0, fmt.Errorf("buscar aplicação: %w", err)
	}

	var joinEUI *string
	if l.JoinEUI != "" {
		joinEUI = &l.JoinEUI
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO sensor (id_aplicacao, codigo_dispositivo, dev_eui, join_eui)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (dev_eui) DO NOTHING`,
		idApp, l.DeviceID, l.DevEUI, joinEUI); err != nil {
		return 0, fmt.Errorf("cadastrar sensor: %w", err)
	}

	id, err = buscarSensor(ctx, tx, l.DevEUI)
	if err != nil {
		return 0, fmt.Errorf("buscar sensor cadastrado: %w", err)
	}
	return id, nil
}

func buscarSensor(ctx context.Context, tx pgx.Tx, devEUI string) (int, error) {
	var id int
	err := tx.QueryRow(ctx, `SELECT id_sensor FROM sensor WHERE dev_eui = $1`, devEUI).Scan(&id)
	return id, err
}

// ListarSensores implementa api.Consultas.
func (r *Repo) ListarSensores(ctx context.Context) ([]modelo.Sensor, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id_sensor, codigo_dispositivo, dev_eui, nome, ativo,
		       ultimo_uplink_em, ST_Y(localizacao), ST_X(localizacao)
		  FROM sensor
		 ORDER BY id_sensor`)
	if err != nil {
		return nil, fmt.Errorf("listar sensores: %w", err)
	}
	defer rows.Close()

	var sensores []modelo.Sensor
	for rows.Next() {
		var s modelo.Sensor
		if err := rows.Scan(&s.ID, &s.CodigoDispositivo, &s.DevEUI, &s.Nome, &s.Ativo,
			&s.UltimoUplinkEm, &s.Latitude, &s.Longitude); err != nil {
			return nil, fmt.Errorf("ler sensor: %w", err)
		}
		sensores = append(sensores, s)
	}
	return sensores, rows.Err()
}

// ConsultarLeituras implementa api.Consultas. Os filtros opcionais entram na
// consulta como parâmetros posicionais, nunca concatenados.
func (r *Repo) ConsultarLeituras(ctx context.Context, f modelo.FiltroLeituras) ([]modelo.Leitura, error) {
	sql := `
		SELECT s.dev_eui, g.codigo, g.unidade, l.medido_em, l.valor, l.mongo_id
		  FROM leitura l
		  JOIN sensor   s ON s.id_sensor   = l.id_sensor
		  JOIN grandeza g ON g.id_grandeza = l.id_grandeza
		 WHERE l.medido_em >= $1 AND l.medido_em < $2`
	args := []any{f.De, f.Ate}

	if f.DevEUI != nil {
		args = append(args, *f.DevEUI)
		sql += fmt.Sprintf(" AND s.dev_eui = $%d", len(args))
	}
	if f.Grandeza != nil {
		args = append(args, *f.Grandeza)
		sql += fmt.Sprintf(" AND g.codigo = $%d", len(args))
	}
	args = append(args, f.Limite)
	sql += fmt.Sprintf(" ORDER BY l.medido_em DESC LIMIT $%d", len(args))

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("consultar leituras: %w", err)
	}
	defer rows.Close()

	var leituras []modelo.Leitura
	for rows.Next() {
		var l modelo.Leitura
		if err := rows.Scan(&l.DevEUI, &l.Grandeza, &l.Unidade, &l.MedidoEm, &l.Valor, &l.MongoID); err != nil {
			return nil, fmt.Errorf("ler leitura: %w", err)
		}
		leituras = append(leituras, l)
	}
	return leituras, rows.Err()
}
