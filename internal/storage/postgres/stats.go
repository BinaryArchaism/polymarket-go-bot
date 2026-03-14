package postgres

import (
	"database/sql"

	"github.com/BinaryArchaism/polymarket-go-bot/internal/model"
)

func (pg *Postgres) InsertStats(tx *sql.Tx, stats model.Stats) error {
	fn := func(tx *sql.Tx) error {
		query := `
			INSERT INTO stats (condition_id, stat_type, stat)
			VALUES ($1, $2, $3)
		`
		_, err := tx.Exec(query, stats.ConditionID, stats.StatType, stats.Stat)
		return err
	}

	if tx != nil {
		return fn(tx)
	}

	return pg.TxWrap(fn)
}
