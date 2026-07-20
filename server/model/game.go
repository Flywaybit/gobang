package model

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const (
	GameStatusOngoing     = "ongoing"
	GameStatusFinished    = "finished"
	GameStatusInterrupted = "interrupted"
)

type Move struct {
	UserID int32     `bson:"user_id"`
	Chess  int32     `bson:"chess"`
	X      int32     `bson:"x"`
	Y      int32     `bson:"y"`
	At     time.Time `bson:"at"`
}

type Game struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	BlackID   int32         `bson:"black_user_id"`
	WhiteID   int32         `bson:"white_user_id"`
	WinnerID  int32         `bson:"winner_id,omitempty"`
	Moves     []Move        `bson:"moves"`
	StartedAt time.Time     `bson:"started_at"`
	EndedAt   *time.Time    `bson:"ended_at,omitempty"`
	Status    string        `bson:"status"`
}
