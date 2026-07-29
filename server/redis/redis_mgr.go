package redis

import (
	"fmt"
	applog "gobang/log"
	"gobang/server/config"
	"sync"
	"time"

	"github.com/gomodule/redigo/redis"
)

type RedisMgr struct {
	pool *redis.Pool
}

var (
	redisOnce sync.Once
	redisMgr  *RedisMgr
)

func GetRedisMgr() *RedisMgr {
	redisOnce.Do(func() {
		cfg := config.GetConfigMgr()
		redisMgr = &RedisMgr{
			pool: &redis.Pool{
				MaxIdle:     cfg.RedisMaxIdle(),
				MaxActive:   cfg.RedisMaxActive(),
				IdleTimeout: time.Duration(cfg.RedisIdleTimeoutSeconds()) * time.Second,
				Dial: func() (redis.Conn, error) {
					conn, err := redis.Dial(
						"tcp",
						cfg.RedisAddr(),
						redis.DialConnectTimeout(3*time.Second),
						redis.DialReadTimeout(3*time.Second),
						redis.DialWriteTimeout(3*time.Second),
					)
					if err != nil {
						return nil, fmt.Errorf("connect Redis failed, addr=%s: %w", cfg.RedisAddr(), err)
					}
					if password := cfg.RedisPassword(); password != "" {
						if _, err := conn.Do("AUTH", password); err != nil {
							_ = conn.Close()
							return nil, fmt.Errorf("auth Redis failed, addr=%s: %w", cfg.RedisAddr(), err)
						}
					}
					if db := cfg.RedisDB(); db > 0 {
						if _, err := conn.Do("SELECT", db); err != nil {
							_ = conn.Close()
							return nil, fmt.Errorf("select Redis db failed, db=%d: %w", db, err)
						}
					}
					return conn, nil
				},
				TestOnBorrow: func(conn redis.Conn, idleTime time.Time) error {
					if time.Since(idleTime) < time.Minute {
						return nil
					}
					_, err := conn.Do("PING")
					return err
				},
			},
		}
		applog.Servicef("Redis connection pool created: addr=%s db=%d max_idle=%d max_active=%d",
			cfg.RedisAddr(), cfg.RedisDB(), cfg.RedisMaxIdle(), cfg.RedisMaxActive())
	})
	return redisMgr
}

func (m *RedisMgr) SetOnline(userID int32, ttlSeconds int) error {
	key := onlineKey(userID)
	conn := m.pool.Get()
	defer conn.Close()
	if err := conn.Err(); err != nil {
		err = fmt.Errorf("get Redis connection failed: %w", err)
		applog.Servicef("Redis SetOnline failed: user_id=%d err=%v", userID, err)
		return err
	}
	if ttlSeconds > 0 {
		_, err := conn.Do("SETEX", key, ttlSeconds, "1")
		if err != nil {
			err = fmt.Errorf("set Redis online key failed, user_id=%d: %w", userID, err)
			applog.Servicef("Redis SetOnline failed: user_id=%d err=%v", userID, err)
			return err
		}
		return nil
	}
	_, err := conn.Do("SET", key, "1")
	if err != nil {
		err = fmt.Errorf("set Redis online key failed, user_id=%d: %w", userID, err)
		applog.Servicef("Redis SetOnline failed: user_id=%d err=%v", userID, err)
	}
	return err
}

func (m *RedisMgr) SetOnlineIfAbsent(userID int32, ttlSeconds int) (bool, error) {
	if ttlSeconds <= 0 {
		return false, fmt.Errorf("online ttl must be positive, user_id=%d ttl=%d", userID, ttlSeconds)
	}
	key := onlineKey(userID)
	conn := m.pool.Get()
	defer conn.Close()
	if err := conn.Err(); err != nil {
		err = fmt.Errorf("get Redis connection failed: %w", err)
		applog.Servicef("Redis SetOnlineIfAbsent failed: user_id=%d err=%v", userID, err)
		return false, err
	}
	reply, err := redis.String(conn.Do("SET", key, "1", "EX", ttlSeconds, "NX"))
	if err == redis.ErrNil {
		return false, nil
	}
	if err != nil {
		err = fmt.Errorf("set Redis online key if absent failed, user_id=%d: %w", userID, err)
		applog.Servicef("Redis SetOnlineIfAbsent failed: user_id=%d err=%v", userID, err)
		return false, err
	}
	return reply == "OK", nil
}

func (m *RedisMgr) RefreshOnline(userID int32, ttlSeconds int) error {
	if ttlSeconds <= 0 {
		return nil
	}
	key := onlineKey(userID)
	conn := m.pool.Get()
	defer conn.Close()
	if err := conn.Err(); err != nil {
		err = fmt.Errorf("get Redis connection failed: %w", err)
		applog.Servicef("Redis RefreshOnline failed: user_id=%d err=%v", userID, err)
		return err
	}
	exists, err := redis.Bool(conn.Do("EXISTS", key))
	if err != nil {
		err = fmt.Errorf("check Redis online key failed, user_id=%d: %w", userID, err)
		applog.Servicef("Redis RefreshOnline failed: user_id=%d err=%v", userID, err)
		return err
	}
	if !exists {
		return nil
	}
	_, err = conn.Do("EXPIRE", key, ttlSeconds)
	if err != nil {
		err = fmt.Errorf("refresh Redis online key failed, user_id=%d: %w", userID, err)
		applog.Servicef("Redis RefreshOnline failed: user_id=%d err=%v", userID, err)
	}
	return err
}

func (m *RedisMgr) DelOnline(userID int32) error {
	conn := m.pool.Get()
	defer conn.Close()
	if err := conn.Err(); err != nil {
		err = fmt.Errorf("get Redis connection failed: %w", err)
		applog.Servicef("Redis DelOnline failed: user_id=%d err=%v", userID, err)
		return err
	}
	_, err := conn.Do("DEL", onlineKey(userID))
	if err != nil {
		err = fmt.Errorf("delete Redis online key failed, user_id=%d: %w", userID, err)
		applog.Servicef("Redis DelOnline failed: user_id=%d err=%v", userID, err)
	}
	return err
}

func (m *RedisMgr) IsOnline(userID int32) (bool, error) {
	conn := m.pool.Get()
	defer conn.Close()
	if err := conn.Err(); err != nil {
		err = fmt.Errorf("get Redis connection failed: %w", err)
		applog.Servicef("Redis IsOnline failed: user_id=%d err=%v", userID, err)
		return false, err
	}
	online, err := redis.Bool(conn.Do("EXISTS", onlineKey(userID)))
	if err != nil {
		err = fmt.Errorf("check Redis online key failed, user_id=%d: %w", userID, err)
		applog.Servicef("Redis IsOnline failed: user_id=%d err=%v", userID, err)
		return false, err
	}
	return online, nil
}

func (m *RedisMgr) Close() error {
	if m == nil || m.pool == nil {
		return nil
	}
	return m.pool.Close()
}

func onlineKey(userID int32) string {
	return fmt.Sprintf("online:user:%d", userID)
}
