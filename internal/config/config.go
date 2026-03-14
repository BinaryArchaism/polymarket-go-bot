package config

import (
	"fmt"
	"os"
	"time"

	"github.com/shopspring/decimal"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Polymarket PolymarketConfig `yaml:"polymarket"`
	Blockchain BlockchainConfig `yaml:"blockchain"`
	Market     MarketConfig     `yaml:"market"`
	Strategy   StrategyConfig   `yaml:"strategy"`
	Database   DatabaseConfig   `yaml:"database"`
	Telegram   TelegramConfig   `yaml:"telegram"`

	allowedUnderlyings map[string]bool
}

type PolymarketConfig struct {
	CLOBURL               string `yaml:"clob_url"`
	GammaURL              string `yaml:"gamma_url"`
	USDCeAddress          string `yaml:"usdce_address"`
	ProxyWalletAddress    string `yaml:"proxy_wallet_address"`
	ConditionTokenAddress string `yaml:"conditional_token_address"`
	CTFExchangeAddress    string `yaml:"ctf_exchange_address"`
}

type BlockchainConfig struct {
	PolygonRPC string `yaml:"polygon_rpc"`
	PrivateKey string // only from env
}

type MarketConfig struct {
	AllowedUnderlying []string      `yaml:"allowed_underlying"`
	UpdateInterval    time.Duration `yaml:"update_interval"`
	PollInterval      time.Duration `yaml:"poll_interval"`
	MinTimeToExpiry   time.Duration `yaml:"min_time_to_expiry"`
	MockOrders        bool          `yaml:"mock_orders"`
}

type StrategyConfig struct {
	RiskLimit decimal.Decimal `yaml:"risk_limit"`
	MinSize   decimal.Decimal `yaml:"min_size"`
	MaxStep   decimal.Decimal `yaml:"max_step"`
	EpsMin    decimal.Decimal `yaml:"eps_min"`
	EpsMax    decimal.Decimal `yaml:"eps_max"`
	TauMin    decimal.Decimal `yaml:"tau_min"`
	TauMax    decimal.Decimal `yaml:"tau_max"`
	KMin      decimal.Decimal `yaml:"k_min"`
	KMax      decimal.Decimal `yaml:"k_max"`
	Alpha     decimal.Decimal `yaml:"alpha"`
	EMax      decimal.Decimal `yaml:"e_max"`
	Debounce  int64           `yaml:"debounce"`
	StopTTE   int64           `yaml:"stop_tte"`
}

type DatabaseConfig struct {
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Host     string `yaml:"host"`
	Port     string `yaml:"port"`
	DBName   string `yaml:"dbname"`
}

type TelegramConfig struct {
	BotToken string `yaml:"bot_token"`
	ChatID   int64  `yaml:"chat_id"`
	Enabled  bool   `yaml:"enabled"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config yaml: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	cfg.Blockchain.PrivateKey = os.Getenv("POLYMARKET_PRIVATE_KEY")

	cfg.buildAllowedUnderlyings()

	return &cfg, nil
}

func (cfg *Config) validate() error {
	if cfg.Polymarket.GammaURL == "" {
		return fmt.Errorf("polymarket.gamma_url is required")
	}
	if cfg.Blockchain.PolygonRPC == "" {
		return fmt.Errorf("blockchain.polygon_rpc is required")
	}
	if cfg.Database.Host == "" {
		return fmt.Errorf("database.host is required")
	}
	if len(cfg.Market.AllowedUnderlying) == 0 {
		return fmt.Errorf("market.allowed_underlying must have at least one value")
	}
	if cfg.Strategy.MinSize.IsZero() || cfg.Strategy.MinSize.IsNegative() {
		return fmt.Errorf("strategy.min_size must be positive")
	}
	return nil
}

func (cfg *Config) buildAllowedUnderlyings() {
	cfg.allowedUnderlyings = make(map[string]bool, len(cfg.Market.AllowedUnderlying))
	for _, u := range cfg.Market.AllowedUnderlying {
		cfg.allowedUnderlyings[u] = true
	}
}

func (cfg *Config) IsAllowedUnderlying(underlying string) bool {
	return cfg.allowedUnderlyings[underlying]
}

func (cfg *Config) GetDBConnURL() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		cfg.Database.User, cfg.Database.Password, cfg.Database.Host,
		cfg.Database.Port, cfg.Database.DBName)
}
