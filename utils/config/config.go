package config

import (
	"log/slog"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

type Config struct {
	Server   Server     `mapstructure:"server"`
	Sandbox  Sandbox    `mapstructure:"sandbox"`
	LogLevel slog.Level `mapstructure:"log_level"`
}

type Server struct {
	GatewayAddress string `mapstructure:"gateway_address"`
	BackendAddress string `mapstructure:"backend_address"`
}

type Sandbox struct {
	AgentSelector   string        `mapstructure:"agentselector"`
	SandboxSelector string        `mapstructure:"sandboxselector"`
	Port            int           `mapstructure:"port"`
	Namespace       string        `mapstructure:"namespace"`
	PollInterval    time.Duration `mapstructure:"poll_interval"`
}

func Load(path string) (*Config, error) {
	cfg := &Config{}
	viper.SetDefault("server.gateway_address", ":8080")
	viper.SetDefault("server.backend_address", "")
	viper.SetDefault("sandbox.agentselector", "agent")
	viper.SetDefault("sandbox.sandboxselector", "sandbox")
	viper.SetConfigFile(path)
	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	hook := viper.DecodeHook(
		mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.TextUnmarshallerHookFunc(),
		),
	)

	if err := viper.Unmarshal(&cfg, hook); err != nil {
		return nil, err
	}

	return cfg, nil
}
