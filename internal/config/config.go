// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
)

type Config struct {
	DBPath              string
	Listen              string
	LogLevel            string
	LogFormat           string
	HonkerExtensionPath string
}

var (
	validLogLevels  = map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	validLogFormats = map[string]bool{"json": true, "text": true}
)

// Load reads configuration from process environment.
func Load() (*Config, error) {
	return load(os.Getenv)
}

func load(getenv func(string) string) (*Config, error) {
	c := &Config{
		DBPath:              or(getenv("WIRE_DB_PATH"), "./wire.db"),
		Listen:              or(getenv("WIRE_LISTEN"), ":8080"),
		LogLevel:            or(getenv("WIRE_LOG_LEVEL"), "info"),
		LogFormat:           or(getenv("WIRE_LOG_FORMAT"), "json"),
		// SQLite's load_extension() appends the platform extension itself, so the path
		// must NOT include `.so`/`.dylib` (passing `libhonker_ext.so` makes SQLite look
		// for `libhonker_ext.so.so`, which doesn't exist).
		HonkerExtensionPath: or(getenv("WIRE_HONKER_EXTENSION_PATH"), "./build/libhonker_ext"),
	}
	if !validLogLevels[c.LogLevel] {
		return nil, fmt.Errorf("invalid WIRE_LOG_LEVEL %q (want debug|info|warn|error)", c.LogLevel)
	}
	if !validLogFormats[c.LogFormat] {
		return nil, fmt.Errorf("invalid WIRE_LOG_FORMAT %q (want json|text)", c.LogFormat)
	}
	return c, nil
}

func or(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
