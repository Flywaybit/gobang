package dao

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Store struct {
	client *mongo.Client
	db     *mongo.Database
	Users  *UserDAO
	Games  *GameDAO
}

func NewStore(ctx context.Context, uri, dbName string) (*Store, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, err
	}
	db := client.Database(dbName)
	store := &Store{
		client: client,
		db:     db,
	}
	store.Users = NewUserDAO(db.Collection("users"))
	store.Games = NewGameDAO(db.Collection("games"))
	return store, nil
}

func (s *Store) EnsureIndexes(ctx context.Context) error {
	if err := s.Users.EnsureIndexes(ctx); err != nil {
		return err
	}
	return s.Games.EnsureIndexes(ctx)
}

func (s *Store) Disconnect(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

func TimeoutContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 2*time.Second)
}
