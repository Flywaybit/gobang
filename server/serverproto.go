package main

import (
	"context"
	"errors"
	"fmt"
	"gobang/pb"
	"gobang/server/dao"
	"gobang/server/model"
	"gobang/server/service"
	"gobang/server/session"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

var (
	manager     = session.NewManager()
	store       *dao.Store
	authService *service.AuthService
	matchSvc    *service.MatchService
	botPool     = service.NewBotPool(10)
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var err error
	store, err = dao.NewStore(ctx, "mongodb://127.0.0.1:27017", "gobang")
	if err != nil {
		fmt.Println("MongoDB connection failed:", err)
		return
	}
	if err := store.EnsureIndexes(ctx); err != nil {
		fmt.Println("MongoDB index initialization failed:", err)
		return
	}
	defer store.Disconnect(context.Background())

	authService = service.NewAuthService(store.Users)
	matchSvc = service.NewMatchService(manager, store.Games, botPool)
	go startWebServer()

	listener, err := net.Listen("tcp", "127.0.0.1:8888")
	if err != nil {
		fmt.Println("TCP listen failed:", err)
		return
	}
	defer listener.Close()
	fmt.Println("Protobuf Gobang TCP server started: 127.0.0.1:8888")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		fmt.Println("Server shutting down...")
		_ = listener.Close()
		closeActiveGames()
		_ = store.Disconnect(context.Background())
		os.Exit(0)
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		player := manager.AddConn(conn)
		player.Send = func(msg *pb.GameMsg) error {
			return sendMsg(conn, msg)
		}
		go handlePlayer(player)
	}
}

func handlePlayer(player *session.PlayerSession) {
	defer func() {
		_, game := manager.RemoveConn(player.Conn)
		if game != nil {
			handleInterruptedGame(game, player)
		}
		_ = player.Conn.Close()
	}()

	for {
		msg, err := readMsg(player.Conn)
		if err != nil {
			fmt.Println("TCP player disconnected:", player.Username)
			return
		}
		handleGameMsg(player, msg)
	}
}

func handleGameMsg(player *session.PlayerSession, msg *pb.GameMsg) {
	switch msg.MsgType {
	case pb.MsgType_MSG_REGISTER_REQ:
		handleRegister(player, msg)
	case pb.MsgType_MSG_LOGIN_REQ:
		handleLogin(player, msg)
	case pb.MsgType_MSG_MATCH_REQ:
		handleMatchRequest(player, msg)
	case pb.MsgType_MSG_PUT:
		handlePut(player, msg)
	case pb.MsgType_MSG_QUIT_GAME:
		handleQuitGame(player)
	default:
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_TIP, Tip: "未知消息类型"})
	}
}

func handleRegister(player *session.PlayerSession, msg *pb.GameMsg) {
	user, err := authService.Register(msg.Username, msg.Password)
	if err != nil {
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_REGISTER_RESP, Tip: err.Error()})
		return
	}
	if err := manager.Authenticate(player, user.UserID, user.Username); err != nil {
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_REGISTER_RESP, Tip: err.Error()})
		return
	}
	sendToPlayer(player, &pb.GameMsg{
		MsgType:  pb.MsgType_MSG_REGISTER_RESP,
		UserId:   user.UserID,
		Username: user.Username,
		Tip:      "注册成功",
	})
}

func handleLogin(player *session.PlayerSession, msg *pb.GameMsg) {
	user, err := authService.Login(msg.Username, msg.Password)
	if err != nil {
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_LOGIN_RESP, Tip: err.Error()})
		return
	}
	if err := manager.Authenticate(player, user.UserID, user.Username); err != nil {
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_LOGIN_RESP, Tip: err.Error()})
		return
	}
	sendToPlayer(player, &pb.GameMsg{
		MsgType:  pb.MsgType_MSG_LOGIN_RESP,
		UserId:   user.UserID,
		Username: user.Username,
		Tip:      "登录成功",
	})
}

func handleMatchRequest(player *session.PlayerSession, msg *pb.GameMsg) {
	if player.State != session.StateAuthenticated {
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_TIP, Tip: "请先登录或注册"})
		return
	}
	if msg.VsBot {
		game, err := matchSvc.JoinBot(player)
		if err != nil {
			sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_TIP, Tip: err.Error()})
			return
		}
		sendToPlayer(game.Black, service.StartMsg(pb.ChessType_BLACK, game.Black.UserID))
		if game.Current == pb.ChessType_WHITE {
			go playBotTurn(game)
		}
		return
	}
	game, matched, err := matchSvc.Join(player)
	if err != nil {
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_TIP, Tip: "匹配失败: " + err.Error()})
		return
	}
	if !matched {
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_WAIT, Tip: "已进入匹配队列，等待对手..."})
		return
	}
	sendToPlayer(game.Black, service.StartMsg(pb.ChessType_BLACK, game.Black.UserID))
	sendToPlayer(game.White, service.StartMsg(pb.ChessType_WHITE, game.White.UserID))
}

func handlePut(player *session.PlayerSession, msg *pb.GameMsg) {
	if player.State != session.StatePlaying {
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_TIP, Tip: "当前不在对局中"})
		return
	}
	game := manager.GetGame(player.GameID)
	if game == nil {
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_TIP, Tip: "对局不存在"})
		return
	}

	x, y := msg.X, msg.Y
	selfChess := player.Chess
	var putMsg *pb.GameMsg
	var win bool
	var tip string

	game.Mu.Lock()
	if game.Finished {
		tip = "对局已结束"
	} else if game.Current != selfChess {
		tip = "未到你的回合，请等待对手"
	} else if x < 0 || x >= Size || y < 0 || y >= Size || game.Board[y][x] != pb.ChessType_EMPTY {
		tip = "落子位置非法，重新输入"
	} else {
		game.Board[y][x] = selfChess
		win = isWinLocked(game, int(x), int(y), selfChess)
		if win {
			game.Finished = true
		} else if game.Current == pb.ChessType_BLACK {
			game.Current = pb.ChessType_WHITE
		} else {
			game.Current = pb.ChessType_BLACK
		}
		putMsg = &pb.GameMsg{
			MsgType: pb.MsgType_MSG_PUT,
			Chess:   selfChess,
			X:       x,
			Y:       y,
			UserId:  player.UserID,
		}
	}
	game.Mu.Unlock()

	if tip != "" {
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_TIP, Tip: tip})
		return
	}

	broadcastGame(game, putMsg)

	move := model.Move{UserID: player.UserID, Chess: int32(selfChess), X: x, Y: y, At: time.Now()}
	go func() {
		ctx, cancel := dao.TimeoutContext()
		defer cancel()
		_ = store.Games.AddMove(ctx, game.DBID, move)
	}()

	if win {
		winnerID := player.UserID
		loser := game.Opponent(player)
		loserID := int32(0)
		if loser != nil {
			loserID = loser.UserID
		}
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_WIN, Chess: selfChess, UserId: winnerID, Tip: "恭喜你获胜！"})
		if loser != nil {
			sendToPlayer(loser, &pb.GameMsg{MsgType: pb.MsgType_MSG_WIN, Chess: selfChess, UserId: winnerID, Tip: "对手五子连珠，你输了"})
		}
		finishGame(game)
		go persistGameResult(game.DBID, winnerID, loserID)
		return
	}
	if game.VsBot && game.Current == pb.ChessType_WHITE {
		go playBotTurn(game)
	}
}

func handleQuitGame(player *session.PlayerSession) {
	if player.State == session.StateMatching {
		manager.CancelMatching(player)
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_TIP, Tip: "已退出匹配队列"})
		return
	}
	if player.GameID == "" {
		player.State = session.StateAuthenticated
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_TIP, Tip: "已退出房间"})
		return
	}
	game := manager.GetGame(player.GameID)
	if game == nil {
		player.State = session.StateAuthenticated
		player.GameID = ""
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_TIP, Tip: "已回到主菜单"})
		return
	}

	opponent := game.Opponent(player)
	var shouldPersist bool
	var winnerID int32

	game.Mu.Lock()
	if !game.Finished {
		game.Finished = true
		shouldPersist = true
		if opponent != nil {
			winnerID = opponent.UserID
		}
	}
	game.Mu.Unlock()

	finishGame(game)
	sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_TIP, Tip: "已退出房间"})
	if opponent != nil {
		sendToPlayer(opponent, &pb.GameMsg{
			MsgType: pb.MsgType_MSG_WIN,
			UserId:  winnerID,
			Tip:     "对手已离开，本局结束",
		})
	}
	if shouldPersist && winnerID != 0 {
		go persistGameResult(game.DBID, winnerID, player.UserID)
	}
}

func persistGameResult(gameID bson.ObjectID, winnerID, loserID int32) {
	if winnerID == 0 {
		return
	}
	ctx, cancel := dao.TimeoutContext()
	defer cancel()
	if winnerID > 0 {
		_ = store.Users.AddWin(ctx, winnerID)
	}
	if loserID > 0 {
		_ = store.Users.AddLoss(ctx, loserID)
	}
	ctx2, cancel2 := dao.TimeoutContext()
	defer cancel2()
	_ = store.Games.Finish(ctx2, gameID, winnerID)
}

func handleInterruptedGame(game *session.GameContext, leaver *session.PlayerSession) {
	opponent := game.Opponent(leaver)
	var shouldPersist bool
	var winnerID int32

	game.Mu.Lock()
	if !game.Finished {
		game.Finished = true
		shouldPersist = true
		if opponent != nil {
			winnerID = opponent.UserID
		}
	}
	game.Mu.Unlock()

	finishGame(game)
	if opponent != nil {
		sendToPlayer(opponent, &pb.GameMsg{
			MsgType: pb.MsgType_MSG_WIN,
			UserId:  winnerID,
			Tip:     "对方退出房间",
		})
	}
	if shouldPersist && winnerID != 0 {
		go persistGameResult(game.DBID, winnerID, leaver.UserID)
		return
	}
	ctx, cancel := dao.TimeoutContext()
	defer cancel()
	_ = store.Games.Interrupt(ctx, game.DBID)
}

func closeActiveGames() {
	for _, game := range manager.ActiveGames() {
		ctx, cancel := dao.TimeoutContext()
		_ = store.Games.Interrupt(ctx, game.DBID)
		cancel()
		if game.Black != nil && game.Black.Conn != nil {
			_ = game.Black.Conn.Close()
		}
		if game.White != nil && game.White.Conn != nil {
			_ = game.White.Conn.Close()
		}
	}
}

func playBotTurn(game *session.GameContext) {
	bot := game.White
	if bot == nil || !bot.IsBot {
		return
	}

	var board [Size][Size]pb.ChessType
	game.Mu.Lock()
	if game.Finished || game.Current != bot.Chess {
		game.Mu.Unlock()
		return
	}
	board = game.Board
	game.Mu.Unlock()

	x, y, ok := service.BestBotMove(board, bot.Chess)
	if !ok {
		return
	}

	var putMsg *pb.GameMsg
	var win bool
	game.Mu.Lock()
	if !game.Finished && game.Current == bot.Chess && x >= 0 && x < Size && y >= 0 && y < Size && game.Board[y][x] == pb.ChessType_EMPTY {
		game.Board[y][x] = bot.Chess
		win = isWinLocked(game, int(x), int(y), bot.Chess)
		if win {
			game.Finished = true
		} else {
			game.Current = pb.ChessType_BLACK
		}
		putMsg = &pb.GameMsg{MsgType: pb.MsgType_MSG_PUT, Chess: bot.Chess, X: x, Y: y, UserId: bot.UserID}
	}
	game.Mu.Unlock()

	if putMsg == nil {
		return
	}
	broadcastGame(game, putMsg)
	move := model.Move{UserID: bot.UserID, Chess: int32(bot.Chess), X: x, Y: y, At: time.Now()}
	go func() {
		ctx, cancel := dao.TimeoutContext()
		defer cancel()
		_ = store.Games.AddMove(ctx, game.DBID, move)
	}()
	if win {
		winnerID := bot.UserID
		loserID := game.BlackID
		sendToPlayer(game.Black, &pb.GameMsg{MsgType: pb.MsgType_MSG_WIN, Chess: bot.Chess, UserId: winnerID, Tip: "Bot 五子连珠，你输了"})
		finishGame(game)
		go persistGameResult(game.DBID, winnerID, loserID)
	}
}

func sendToPlayer(player *session.PlayerSession, msg *pb.GameMsg) {
	if player == nil || player.IsBot {
		return
	}
	if player.Send != nil {
		_ = player.Send(msg)
		return
	}
	if player.Conn != nil {
		_ = sendMsg(player.Conn, msg)
	}
}

func broadcastGame(game *session.GameContext, msg *pb.GameMsg) {
	sendToPlayer(game.Black, msg)
	sendToPlayer(game.White, msg)
}

func finishGame(game *session.GameContext) {
	manager.FinishGame(game)
	if game.Black != nil && game.Black.IsBot {
		matchSvc.ReleaseBot(game.Black)
	}
	if game.White != nil && game.White.IsBot {
		matchSvc.ReleaseBot(game.White)
	}
}

func isWinLocked(game *session.GameContext, x, y int, chess pb.ChessType) bool {
	dirs := [][]int{{1, 0}, {0, 1}, {1, 1}, {1, -1}}
	for _, d := range dirs {
		cnt := 1
		dx, dy := d[0], d[1]
		tmpx, tmpy := x+dx, y+dy
		for tmpx >= 0 && tmpx < Size && tmpy >= 0 && tmpy < Size && game.Board[tmpy][tmpx] == chess {
			cnt++
			tmpx += dx
			tmpy += dy
		}
		tmpx, tmpy = x-dx, y-dy
		for tmpx >= 0 && tmpx < Size && tmpy >= 0 && tmpy < Size && game.Board[tmpy][tmpx] == chess {
			cnt++
			tmpx -= dx
			tmpy -= dy
		}
		if cnt >= 5 {
			return true
		}
	}
	return false
}
