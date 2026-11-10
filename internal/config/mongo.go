package config

// Mongo configura apenas o acesso ao MongoDB (usado pela carga de arquivo).
type Mongo struct {
	URI string
	DB  string
}

// CarregarMongo lê MONGO_URI e MONGO_DB.
func CarregarMongo() (Mongo, error) {
	var l leitor
	c := Mongo{
		URI: l.obrigatorio("MONGO_URI"),
		DB:  l.texto("MONGO_DB", "tcc_bruto"),
	}
	return c, l.erro()
}
