package service

import (
	"errors"
	"fmt"
	"gobang/pb"
	"gobang/server/session"
	"sync"
)

type BotPool struct {
	mu   sync.Mutex
	bots []*session.PlayerSession
	free map[int32]bool
}

func NewBotPool(size int) *BotPool {
	pool := &BotPool{free: make(map[int32]bool)}
	for i := 1; i <= size; i++ {
		bot := &session.PlayerSession{
			UserID:   int32(-i),
			Username: fmt.Sprintf("Bot_%d", i),
			State:    session.StateAuthenticated,
			IsBot:    true,
		}
		pool.bots = append(pool.bots, bot)
		pool.free[bot.UserID] = true
	}
	return pool
}

func (p *BotPool) Acquire() (*session.PlayerSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, bot := range p.bots {
		if p.free[bot.UserID] {
			p.free[bot.UserID] = false
			return bot, nil
		}
	}
	return nil, errors.New("Bot 池已满，请稍后重试")
}

func (p *BotPool) Release(bot *session.PlayerSession) {
	if bot == nil || !bot.IsBot {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	bot.State = session.StateAuthenticated
	bot.GameID = ""
	bot.Chess = pb.ChessType_EMPTY
	p.free[bot.UserID] = true
}
