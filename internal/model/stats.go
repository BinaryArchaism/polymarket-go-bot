package model

import "encoding/json"

type Stats struct {
	ConditionID string
	StatType    string
	Stat        json.RawMessage
}
