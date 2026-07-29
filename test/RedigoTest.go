//go:build redigotest

package main

import (
	"fmt"
	"gobang/server/config"
	"time"

	"github.com/gomodule/redigo/redis"
)

func main() {
	addr := config.GetConfigMgr().RedisAddr()
	conn, err := redis.Dial("tcp", addr, redis.DialConnectTimeout(3*time.Second))
	if err != nil {
		panic(fmt.Sprintf("connect Redis failed, addr=%s err=%v", addr, err))
	}
	defer conn.Close()

	pong, err := redis.String(conn.Do("PING"))
	if err != nil {
		panic(err)
	}
	fmt.Println("PING:", pong)

	key := "gobang:redigo:test:string"
	value := fmt.Sprintf("hello-redigo-%d", time.Now().Unix())
	if _, err := conn.Do("SET", key, value); err != nil {
		panic(err)
	}
	got, err := redis.String(conn.Do("GET", key))
	if err != nil {
		panic(err)
	}
	fmt.Println("GET:", got)

	if _, err := conn.Do("EXPIRE", key, 60); err != nil {
		panic(err)
	}
	ttl, err := redis.Int(conn.Do("TTL", key))
	if err != nil {
		panic(err)
	}
	fmt.Println("TTL:", ttl)

	hashKey := "gobang:redigo:test:hash"
	if _, err := conn.Do("HSET", hashKey, "username", "player1", "score", 100); err != nil {
		panic(err)
	}
	hash, err := redis.StringMap(conn.Do("HGETALL", hashKey))
	if err != nil {
		panic(err)
	}
	fmt.Println("HASH:", hash)

	listKey := "gobang:redigo:test:list"
	if _, err := conn.Do("DEL", listKey); err != nil {
		panic(err)
	}
	if _, err := conn.Do("RPUSH", listKey, "black", "white", "draw"); err != nil {
		panic(err)
	}
	items, err := redis.Strings(conn.Do("LRANGE", listKey, 0, -1))
	if err != nil {
		panic(err)
	}
	fmt.Println("LIST:", items)

	deleted, err := redis.Int(conn.Do("DEL", key, hashKey, listKey))
	if err != nil {
		panic(err)
	}
	fmt.Println("DEL:", deleted)
	fmt.Println("Redigo test passed")
}
