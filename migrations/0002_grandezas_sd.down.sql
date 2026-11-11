-- Reverte 0002_grandezas_sd.up.sql. As grandezas "temperatura" e "umidade"
-- (criadas em 0001) NÃO são tocadas.

-- Remove qualquer leitura pendente que ainda referencie as grandezas que
-- serão apagadas. Em uma base em produção, considere exportar esses dados
-- antes de rodar o down.
DELETE FROM leitura
 WHERE id_grandeza IN (
     SELECT id_grandeza FROM grandeza
      WHERE codigo IN ('vibracao','bateria','pressao','inclinacao','solo')
 );

DELETE FROM grandeza
 WHERE codigo IN ('vibracao','bateria','pressao','inclinacao','solo');
