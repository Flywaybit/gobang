//go:build mongodbtest

package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type PlayerRecord struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	Name      string        `bson:"name"`
	Chess     string        `bson:"chess"`
	Score     int           `bson:"score"`
	CreatedAt time.Time     `bson:"created_at"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI("mongodb://127.0.0.1:27017"))
	if err != nil {
		log.Fatal("连接 MongoDB 失败:", err)
	}
	defer func() {
		if err := client.Disconnect(context.Background()); err != nil {
			log.Println("关闭 MongoDB 连接失败:", err)
		}
	}()

	if err := client.Ping(ctx, nil); err != nil {
		log.Fatal("Ping MongoDB 失败，请确认 MongoDB 服务已启动:", err)
	}
	fmt.Println("MongoDB 连接成功")

	collection := client.Database("gobang").Collection("players")
	record := PlayerRecord{
		Name:      "测试玩家",
		Chess:     "黑棋",
		Score:     100,
		CreatedAt: time.Now(),
	}

	insertResult, err := collection.InsertOne(ctx, record)
	if err != nil {
		log.Fatal("插入测试数据失败:", err)
	}
	fmt.Println("插入测试数据成功，ID:", insertResult.InsertedID)

	var found PlayerRecord
	filter := bson.M{"_id": insertResult.InsertedID}
	if err := collection.FindOne(ctx, filter).Decode(&found); err != nil {
		log.Fatal("查询测试数据失败:", err)
	}
	fmt.Printf("查询结果：玩家=%s，棋子=%s，积分=%d\n", found.Name, found.Chess, found.Score)

	// if _, err := collection.DeleteOne(ctx, filter); err != nil {
	// 	log.Fatal("清理测试数据失败:", err)
	// }
	// fmt.Println("测试数据已清理")
}
