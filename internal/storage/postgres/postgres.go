package postgres

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"time"

	"github.com/BinaryArchaism/polymarket-go-bot/internal/config"

	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog"
)

const pingInterval = 5 * time.Second

type Postgres struct {
	cfg  config.Config
	log  zerolog.Logger
	conn *sql.DB
}

//go:embed migrations/*.sql
var migrations embed.FS

func New(ctx context.Context, log zerolog.Logger, cfg config.Config) (*Postgres, error) {
	logger := log.With().Str("component", "storage").Logger()
	pgconn, err := sql.Open("postgres", cfg.GetDBConnURL())
	if err != nil {
		return nil, fmt.Errorf("can not open pg connection: %w", err)
	}
	if err = pgconn.Ping(); err != nil {
		return nil, fmt.Errorf("can not ping pg connection: %w", err)
	}

	pg := &Postgres{
		cfg:  cfg,
		log:  logger,
		conn: pgconn,
	}

	if err = pg.runMigrations(); err != nil {
		return nil, fmt.Errorf("can not run migrations: %w", err)
	}

	go pg.startHealthCheck(ctx)

	return pg, nil
}

func (pg *Postgres) runMigrations() error {
	pg.log.Info().Msg("starting database migrations")

	goose.SetBaseFS(migrations)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set dialect: %w", err)
	}

	if err := goose.Up(pg.conn, "migrations"); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	pg.log.Info().Msg("database migrations completed")
	return nil
}

func (pg *Postgres) startHealthCheck(ctx context.Context) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := pg.conn.Ping(); err != nil {
				pg.log.Error().Err(err).Msg("can not ping pg connection")
			}
		}
	}
}

func (pg *Postgres) TxWrap(fn func(tx *sql.Tx) error) error {
	tx, err := pg.conn.Begin()
	if err != nil {
		return fmt.Errorf("can not init tx: %w", err)
	}
	defer tx.Rollback()

	if err = fn(tx); err != nil {
		return fmt.Errorf("failed to execute tx: %w", err)
	}
	return tx.Commit()
}
