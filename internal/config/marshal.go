package config

import toml "github.com/pelletier/go-toml/v2"

func marshalTOML(cfg *Config) ([]byte, error) {
	return toml.Marshal(cfg)
}
