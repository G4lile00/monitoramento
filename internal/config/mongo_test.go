package config

import "testing"

func TestCarregarMongo(t *testing.T) {
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("MONGO_DB", "")

	c, err := CarregarMongo()
	if err != nil {
		t.Fatal(err)
	}
	if c.DB != "tcc_bruto" {
		t.Errorf("DB = %q", c.DB)
	}

	t.Setenv("MONGO_URI", "")
	if _, err := CarregarMongo(); err == nil {
		t.Error("esperava erro sem MONGO_URI")
	}
}
