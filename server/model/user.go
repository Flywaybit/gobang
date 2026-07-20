package model

import "go.mongodb.org/mongo-driver/v2/bson"

type User struct {
	ID           bson.ObjectID `bson:"_id,omitempty"`
	UserID       int32         `bson:"user_id"`
	Username     string        `bson:"username"`
	PasswordHash string        `bson:"password_hash"`
	Wins         int32         `bson:"wins"`
	Losses       int32         `bson:"losses"`
}
