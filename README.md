# polymarket-go-bot

> **Disclaimer:** This software is provided for educational and research purposes only. Trading on prediction markets involves significant financial risk. Use at your own risk. The authors are not responsible for any financial losses incurred through the use of this software.
>
> The original codebase was messy, so I used Claude to refactor it for open source. Some functionality may have been lost in the process.

Automated trading bot for [Polymarket](https://polymarket.com) crypto prediction markets (BTC, ETH, SOL, XRP).

## Background

I developed this bot over the winter of 2025/2026 as a side project to explore algorithmic trading on prediction markets. The strategy hasn't shown great results in practice, but the process of building it - designing the tilt-based algorithm, integrating with Polymarket's CLOB and Gnosis Safe on-chain infrastructure, and backtesting against real market data - was a genuinely interesting challenge. I'm sharing it in case others find it useful to learn from or build upon.

## How It Works

The bot trades binary outcome markets (Up/Down) on Polymarket's CLOB exchange using a tilt-based market-making algorithm.

### Algorithm

1. **Fair Price** - Derives `pFair` from mid-prices of Up and Down tokens
2. **Target Tilt** - Computes ideal portfolio allocation between Up/Down using `pFair`, time-to-expiry (TTE), and a configurable aggressiveness curve (`k`, `alpha`)
3. **Money Tilt** - Measures current portfolio allocation by dollar cost
4. **Signal** - Trades when `|targetTilt - moneyTilt| > epsilon` (epsilon shrinks as market nears expiry)
5. **Sizing** - Limits order size by risk headroom and validates via risk simulation before placing
6. **Execution** - Places Fill-and-Kill (FAK) orders through Polymarket's CLOB API

Key parameters: `k_min/k_max` (aggressiveness), `tau_min/tau_max` (tilt bounds), `eps_min/eps_max` (trading threshold), `risk_limit` (max $ risk per market).

### Architecture

```
cmd/bot/main.go          Entry point
internal/
  engine/                Orchestrator + per-market workers + strategy math
  exchange/polymarket/   CLOB API client (orders, prices, redemption)
  storage/postgres/      PostgreSQL persistence (markets, orders, stats)
  balance/               On-chain USDC balance tracking
  notify/telegram/       Telegram notifications
  config/                YAML configuration
  model/                 Domain types
```

## Quickstart

### Prerequisites

- Go 1.25+
- PostgreSQL 16+
- Polymarket account with a funded proxy wallet (Gnosis Safe)
- Polygon RPC endpoint

### Setup

```bash
cp config.example.yaml config.yaml
cp .env.example .env

# Edit config.yaml with your settings
# Edit .env with your private key
```

### Run

```bash
# Local
make run

# Docker
docker compose up -d
```

### Configuration

See `config.example.yaml` for all available options. Key sections:

- **strategy** - Algorithm parameters (risk limits, aggressiveness, thresholds)
- **market** - Which underlyings to trade, polling intervals
- **blockchain** - Polygon RPC, wallet addresses
- **telegram** - Optional notifications

## Development

```bash
make build    # Build binary
make test     # Run tests
make vet      # Run go vet
```

### Backtesting

The test suite includes a backtesting framework that replays historical market data:

```bash
# Run backtest against all markets in local DB
go test ./internal/engine/ -run Test_backtest -v

# Specific market
BT_CONDITION_ID=0x... go test ./internal/engine/ -run Test_backtest -v
```

## License

MIT
