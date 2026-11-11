-- Catálogo adicional de grandezas para o sensor de deslizamento (ChirpStack v4).
-- As grandezas "temperatura" e "umidade" já existem desde 0001_init.up.sql e
-- são mantidas; aqui só adicionamos as novas.
--
-- Idempotente: ON CONFLICT DO NOTHING permite reexecução.

INSERT INTO grandeza (codigo, nome, unidade) VALUES
    ('vibracao',    'Vibração (pico)',           'm/s²'),
    ('bateria',     'Tensão da bateria',         'V'),
    ('pressao',     'Pressão atmosférica',       'hPa'),
    ('inclinacao',  'Inclinação do sensor',      '°'),
    ('solo',        'Umidade do solo',           '%')
ON CONFLICT (codigo) DO NOTHING;
