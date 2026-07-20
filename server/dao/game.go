package dao

import (
	"context"
	"gobang/server/model"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type GameDAO struct {
	coll *mongo.Collection
}

func NewGameDAO(coll *mongo.Collection) *GameDAO {
	return &GameDAO{coll: coll}
}

func (d *GameDAO) EnsureIndexes(ctx context.Context) error {
	_, err := d.coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "black_user_id", Value: 1},
			{Key: "white_user_id", Value: 1},
		},
	})
	return err
}

func (d *GameDAO) CreateOngoing(ctx context.Context, blackID, whiteID int32) (bson.ObjectID, error) {
	game := model.Game{
		BlackID:   blackID,
		WhiteID:   whiteID,
		Moves:     []model.Move{},
		StartedAt: time.Now(),
		Status:    model.GameStatusOngoing,
	}
	result, err := d.coll.InsertOne(ctx, game)
	if err != nil {
		return bson.NilObjectID, err
	}
	id, _ := result.InsertedID.(bson.ObjectID)
	return id, nil
}

func (d *GameDAO) AddMove(ctx context.Context, gameID bson.ObjectID, move model.Move) error {
	_, err := d.coll.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: gameID}},
		bson.D{{Key: "$push", Value: bson.D{{Key: "moves", Value: move}}}},
	)
	return err
}

func (d *GameDAO) Finish(ctx context.Context, gameID bson.ObjectID, winnerID int32) error {
	now := time.Now()
	_, err := d.coll.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: gameID}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "winner_id", Value: winnerID},
			{Key: "status", Value: model.GameStatusFinished},
			{Key: "ended_at", Value: now},
		}}},
	)
	return err
}

func (d *GameDAO) Interrupt(ctx context.Context, gameID bson.ObjectID) error {
	now := time.Now()
	_, err := d.coll.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: gameID}, {Key: "status", Value: model.GameStatusOngoing}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "status", Value: model.GameStatusInterrupted},
			{Key: "ended_at", Value: now},
		}}},
	)
	return err
}
