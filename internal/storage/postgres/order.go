package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/BinaryArchaism/polymarket-go-bot/internal/model"
)

func (pg *Postgres) InsertOrder(tx *sql.Tx, order model.Order) error {
	fn := func(tx *sql.Tx) error {
		query := `
			INSERT INTO orders (id, status, error, error_msg, condition_id,
			                    original_size, matched_size, price, token_id, outcome, associate_trades)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`
		_, err := tx.Exec(query,
			order.ID, order.Status, order.Error, order.ErrorMsg,
			order.ConditionID, order.OriginalSize, order.MatchedSize,
			order.Price, order.TokenID, order.Outcome, order.AssociateTrades,
		)
		return err
	}

	if tx != nil {
		return fn(tx)
	}

	return pg.TxWrap(fn)
}

func (pg *Postgres) GetOrdersByCondition(conditionID string) ([]model.Order, error) {
	query := `
		SELECT id, status, error, error_msg, condition_id,
		       original_size, matched_size, price, token_id, outcome, associate_trades
		FROM orders
		WHERE condition_id = $1
		ORDER BY created_at DESC
	`
	rows, err := pg.conn.QueryContext(context.Background(), query, conditionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get orders: %w", err)
	}
	defer rows.Close()

	var orders []model.Order
	for rows.Next() {
		var o model.Order
		var errorMsg, associateTrades sql.NullString
		err = rows.Scan(
			&o.ID, &o.Status, &o.Error, &errorMsg, &o.ConditionID,
			&o.OriginalSize, &o.MatchedSize, &o.Price, &o.TokenID,
			&o.Outcome, &associateTrades,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan order: %w", err)
		}
		if errorMsg.Valid {
			o.ErrorMsg = errorMsg.String
		}
		if associateTrades.Valid {
			o.AssociateTrades = associateTrades.String
		}
		orders = append(orders, o)
	}
	return orders, rows.Err()
}
