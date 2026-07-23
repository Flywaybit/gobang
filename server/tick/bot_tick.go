package tick

import (
	"context"
	applog "gobang/log"
	"gobang/pb"
	"gobang/server/session"
	"sync"
	"time"
)

type GameManager interface {
	ActiveGames() []*session.GameContext
}

type BotMoveFunc func(*session.GameContext)

type BotTicker struct {
	manager    GameManager
	moveBot    BotMoveFunc
	processing sync.Map
}

func NewBotTicker(manager GameManager, moveBot BotMoveFunc) *BotTicker {
	return &BotTicker{
		manager: manager,
		moveBot: moveBot,
	}
}

func (t *BotTicker) Start(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.scan()
		}
	}
}

func (t *BotTicker) scan() {
	for _, game := range t.manager.ActiveGames() {
		if !isBotTurn(game) {
			continue
		}
		if _, loaded := t.processing.LoadOrStore(game.ID, struct{}{}); loaded {
			continue
		}
		go func(game *session.GameContext) {
			defer t.processing.Delete(game.ID)
			applog.Servicef("Bot move triggered by tick: game_id=%s", game.ID)
			t.moveBot(game)
		}(game)
	}
}

func isBotTurn(game *session.GameContext) bool {
	if game == nil || !game.VsBot {
		return false
	}
	bot := game.White
	if bot == nil || !bot.IsBot {
		return false
	}

	game.Mu.Lock()
	defer game.Mu.Unlock()
	return !game.Finished && game.Current == bot.Chess && game.Current != pb.ChessType_EMPTY
}
