package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/BinaryArchaism/polymarket-go-bot/internal/model"
)

func (pg *Postgres) GetMarket(marketID string) (model.Market, error) {
	query := `
		SELECT id, event_id, question, condition_id, slug, start_time, end_time,
		       token_up, token_down, status, underlying
		FROM markets
		WHERE id = $1
	`

	var m model.Market
	err := pg.conn.QueryRowContext(context.Background(), query, marketID).Scan(
		&m.ID, &m.EventID, &m.Question, &m.ConditionID, &m.Slug,
		&m.StartTime, &m.EndTime, &m.TokenUp, &m.TokenDown,
		&m.Status, &m.Underlying,
	)
	if err != nil {
		return model.Market{}, fmt.Errorf("failed to get market: %w", err)
	}
	return m, nil
}

func (pg *Postgres) GetResolvedMarkets() ([]model.Market, error) {
	query := `
		SELECT id, event_id, question, condition_id, slug, start_time, end_time,
		       token_up, token_down, status, underlying
		FROM markets
		WHERE status = 'resolved'
		ORDER BY created_at ASC
	`
	rows, err := pg.conn.QueryContext(context.Background(), query)
	if err != nil {
		return nil, fmt.Errorf("failed to get resolved markets: %w", err)
	}
	defer rows.Close()

	var markets []model.Market
	for rows.Next() {
		var m model.Market
		err = rows.Scan(
			&m.ID, &m.EventID, &m.Question, &m.ConditionID, &m.Slug,
			&m.StartTime, &m.EndTime, &m.TokenUp, &m.TokenDown,
			&m.Status, &m.Underlying,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan market: %w", err)
		}
		markets = append(markets, m)
	}
	return markets, rows.Err()
}

func (pg *Postgres) GetPlannedAndActiveMarkets() ([]string, error) {
	query := `
		SELECT id
		FROM markets
		WHERE status IN ('planned', 'active')
		ORDER BY created_at ASC
	`
	rows, err := pg.conn.QueryContext(context.Background(), query)
	if err != nil {
		return nil, fmt.Errorf("failed to get planned and active markets: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan market id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (pg *Postgres) InsertMarket(tx *sql.Tx, market model.Market) error {
	fn := func(tx *sql.Tx) error {
		query := `
			INSERT INTO markets (id, event_id, question, condition_id, slug,
			                     start_time, end_time, token_up, token_down, status, underlying)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`
		_, err := tx.Exec(query,
			market.ID, market.EventID, market.Question, market.ConditionID, market.Slug,
			market.StartTime, market.EndTime, market.TokenUp, market.TokenDown,
			market.Status, market.Underlying,
		)
		return err
	}

	if tx != nil {
		return fn(tx)
	}

	return pg.TxWrap(fn)
}

func (pg *Postgres) UpdateMarketStatus(tx *sql.Tx, marketID string, status model.MarketStatus) error {
	fn := func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE markets SET status = $1, updated_at = NOW() WHERE id = $2`, status, marketID)
		return err
	}

	if tx != nil {
		return fn(tx)
	}

	return pg.TxWrap(fn)
}
