package dao

import (
	"context"
	"gobang/server/model"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type UserDAO struct {
	coll *mongo.Collection
}

func NewUserDAO(coll *mongo.Collection) *UserDAO {
	return &UserDAO{coll: coll}
}

func (d *UserDAO) EnsureIndexes(ctx context.Context) error {
	_, err := d.coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "username", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	return err
}

func (d *UserDAO) NextUserID(ctx context.Context) (int32, error) {
	opts := options.FindOne().SetSort(bson.D{{Key: "user_id", Value: -1}})
	var user model.User
	err := d.coll.FindOne(ctx, bson.D{}, opts).Decode(&user)
	if err == mongo.ErrNoDocuments {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	return user.UserID + 1, nil
}

func (d *UserDAO) Create(ctx context.Context, user *model.User) error {
	_, err := d.coll.InsertOne(ctx, user)
	return err
}

func (d *UserDAO) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	var user model.User
	err := d.coll.FindOne(ctx, bson.D{{Key: "username", Value: username}}).Decode(&user)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (d *UserDAO) AddWin(ctx context.Context, userID int32) error {
	_, err := d.coll.UpdateOne(ctx,
		bson.D{{Key: "user_id", Value: userID}},
		bson.D{{Key: "$inc", Value: bson.D{{Key: "wins", Value: 1}}}},
	)
	return err
}

func (d *UserDAO) AddLoss(ctx context.Context, userID int32) error {
	_, err := d.coll.UpdateOne(ctx,
		bson.D{{Key: "user_id", Value: userID}},
		bson.D{{Key: "$inc", Value: bson.D{{Key: "losses", Value: 1}}}},
	)
	return err
}
