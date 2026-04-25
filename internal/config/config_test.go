package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoad_Defaults(t *testing.T) {
	cfg, err := load(func(string) string { return "" })
	assert.NoError(t, err)
	assert.Equal(t, "./wire.db", cfg.DBPath)
	assert.Equal(t, ":8080", cfg.Listen)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "json", cfg.LogFormat)
	assert.Equal(t, "./build/libhonker_ext", cfg.HonkerExtensionPath)
}

func TestLoad_Overrides(t *testing.T) {
	env := map[string]string{
		"WIRE_DB_PATH":               "/data/wire.db",
		"WIRE_LISTEN":                ":9000",
		"WIRE_LOG_LEVEL":             "debug",
		"WIRE_LOG_FORMAT":            "text",
		"WIRE_HONKER_EXTENSION_PATH": "/usr/local/lib/libhonker_extension.so",
	}
	cfg, err := load(func(k string) string { return env[k] })
	assert.NoError(t, err)
	assert.Equal(t, "/data/wire.db", cfg.DBPath)
	assert.Equal(t, ":9000", cfg.Listen)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, "text", cfg.LogFormat)
	assert.Equal(t, "/usr/local/lib/libhonker_extension.so", cfg.HonkerExtensionPath)
}

func TestLoad_RejectsInvalidLogLevel(t *testing.T) {
	cfg, err := load(func(k string) string {
		if k == "WIRE_LOG_LEVEL" {
			return "loud"
		}
		return ""
	})
	assert.Error(t, err)
	assert.Nil(t, cfg)
}

func TestLoad_RejectsInvalidLogFormat(t *testing.T) {
	_, err := load(func(k string) string {
		if k == "WIRE_LOG_FORMAT" {
			return "yaml"
		}
		return ""
	})
	assert.Error(t, err)
}
