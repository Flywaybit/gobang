package service

import (
	"gobang/pb"
	"gobang/server/dao"
	"gobang/server/session"
)

type MatchService struct {
	manager *session.Manager
	games   *dao.GameDAO
	bots    *BotPool
}

func NewMatchService(manager *session.Manager, games *dao.GameDAO, bots *BotPool) *MatchService {
	return &MatchService{manager: manager, games: games, bots: bots}
}

func (s *MatchService) Join(player *session.PlayerSession) (*session.GameContext, bool, error) {
	opponent := s.manager.Enqueue(player)
	if opponent == nil {
		return nil, false, nil
	}
	ctx, cancel := dao.TimeoutContext()
	defer cancel()
	gameID, err := s.games.CreateOngoing(ctx, opponent.UserID, player.UserID)
	if err != nil {
		return nil, false, err
	}
	game := s.manager.CreateGame(opponent, player, gameID)
	return game, true, nil
}

func (s *MatchService) JoinBot(player *session.PlayerSession) (*session.GameContext, error) {
	bot, err := s.bots.Acquire()
	if err != nil {
		return nil, err
	}
	ctx, cancel := dao.TimeoutContext()
	defer cancel()
	gameID, err := s.games.CreateOngoing(ctx, player.UserID, bot.UserID)
	if err != nil {
		s.bots.Release(bot)
		return nil, err
	}
	return s.manager.CreateGameWithMode(player, bot, gameID, true), nil
}

func (s *MatchService) ReleaseBot(bot *session.PlayerSession) {
	s.bots.Release(bot)
}

func StartMsg(chess pb.ChessType, userID int32) *pb.GameMsg {
	tip := "对局开始，你是白棋后手"
	if chess == pb.ChessType_BLACK {
		tip = "对局开始，你是黑棋先手"
	}
	return &pb.GameMsg{
		MsgType: pb.MsgType_MSG_START,
		Chess:   chess,
		UserId:  userID,
		Tip:     tip,
	}
}
