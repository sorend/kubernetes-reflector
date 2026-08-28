package config

import (
	"os"
	"strconv"
)

const (
	envWatcherTimeout     = "REFLECTOR_WATCHER_TIMEOUT"
	envExcludedNamespaces = "REFLECTOR_EXCLUDED_NAMESPACES"
	envSkipTLSVerify      = "REFLECTOR_SKIP_TLS_VERIFY"
	envLogLevel           = "REFLECTOR_LOG_LEVEL"
)

type Config struct {
	WatcherTimeout     int
	ExcludedNamespaces string
	SkipTLSVerify      bool
	LogLevel           string
}

func Load() Config {
	cfg := Config{
		WatcherTimeout:     3600,
		ExcludedNamespaces: "",
		SkipTLSVerify:      false,
		LogLevel:           "info",
	}

	if value := os.Getenv(envWatcherTimeout); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			cfg.WatcherTimeout = parsed
		}
	}

	if value := os.Getenv(envExcludedNamespaces); value != "" {
		cfg.ExcludedNamespaces = value
	}

	if value := os.Getenv(envSkipTLSVerify); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.SkipTLSVerify = parsed
		}
	}

	if value := os.Getenv(envLogLevel); value != "" {
		cfg.LogLevel = value
	}

	return cfg
}
