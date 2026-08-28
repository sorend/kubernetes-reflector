package config

import (
	"os"
	"strconv"
)

const (
	envWatcherTimeout     = "ES_Reflector__Watcher__Timeout"
	envExcludedNamespaces = "ES_Reflector__Watcher__ExcludedNamespaces"
	envSkipTLSVerify      = "ES_Ignite__KubernetesClient__SkipTlsVerify"
	envLogLevel           = "ES_Serilog__MinimumLevel__Default"
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
		LogLevel:           "Information",
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
