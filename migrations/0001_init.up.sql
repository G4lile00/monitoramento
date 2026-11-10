-- Modelo relacional da plataforma (modelo_banco_hibrido.dbml).
-- Idempotente: pode ser executado mais de uma vez sem erro.

CREATE EXTENSION IF NOT EXISTS timescaledb;
CREATE EXTENSION IF NOT EXISTS postgis;

DO $$
BEGIN
    CREATE TYPE papel_usuario AS ENUM ('administrador', 'operador', 'leitor');
EXCEPTION
    WHEN duplicate_object THEN NULL;
END
$$;

-- ---------- acesso e cadastro ----------

CREATE TABLE IF NOT EXISTS usuario (
    id_usuario      integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    nome            text          NOT NULL,
    email           text          NOT NULL UNIQUE,
    senha_hash      text          NOT NULL,
    papel           papel_usuario NOT NULL DEFAULT 'leitor',
    ativo           boolean       NOT NULL DEFAULT true,
    ultimo_login_em timestamptz,
    criado_em       timestamptz   NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS aplicacao (
    id_aplicacao     integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    codigo_aplicacao text NOT NULL UNIQUE,  -- end_device_ids.application_ids.application_id
    nome             text,
    descricao        text
);

CREATE TABLE IF NOT EXISTS sensor (
    id_sensor          integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    id_aplicacao       integer     NOT NULL REFERENCES aplicacao (id_aplicacao),
    codigo_dispositivo text        NOT NULL,  -- end_device_ids.device_id
    dev_eui            char(16)    NOT NULL UNIQUE,
    join_eui           char(16),
    nome               text,
    descricao          text,
    localizacao        geometry(Point, 4326),
    altitude_m         numeric(7, 2),
    ativo              boolean     NOT NULL DEFAULT true,
    cadastrado_por     integer     REFERENCES usuario (id_usuario),
    criado_em          timestamptz NOT NULL DEFAULT now(),
    atualizado_em      timestamptz NOT NULL DEFAULT now(),
    ultimo_uplink_em   timestamptz,
    UNIQUE (id_aplicacao, codigo_dispositivo)
);

CREATE INDEX IF NOT EXISTS ix_sensor_localizacao ON sensor USING gist (localizacao);

-- ---------- catálogo de grandezas ----------

CREATE TABLE IF NOT EXISTS grandeza (
    id_grandeza smallint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    codigo      text NOT NULL UNIQUE,  -- como vem no texto da mensagem
    nome        text NOT NULL,
    unidade     text NOT NULL
);

INSERT INTO grandeza (codigo, nome, unidade) VALUES
    ('temperatura', 'Temperatura',      'C'),
    ('umidade',     'Umidade relativa', '%')
ON CONFLICT (codigo) DO NOTHING;

-- ---------- leituras (formato longo; hipertabela TimescaleDB) ----------
-- A chave primária inclui medido_em (exigência do TimescaleDB) e garante a
-- idempotência do worker: reprocessar o mesmo documento não duplica leituras.
-- mongo_id referencia logicamente mensagens_brutas._id (sem chave estrangeira).

CREATE TABLE IF NOT EXISTS leitura (
    id_sensor   integer          NOT NULL REFERENCES sensor (id_sensor),
    id_grandeza smallint         NOT NULL REFERENCES grandeza (id_grandeza),
    medido_em   timestamptz      NOT NULL,
    inserido_em timestamptz      NOT NULL DEFAULT now(),
    valor       double precision NOT NULL,
    mongo_id    char(24)         NOT NULL,
    PRIMARY KEY (mongo_id, id_grandeza, medido_em)
);

SELECT create_hypertable('leitura', 'medido_em', if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS ix_leitura_sensor_tempo
    ON leitura (id_sensor, id_grandeza, medido_em DESC);
