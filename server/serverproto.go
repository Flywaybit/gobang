package main

import (
	"context"
	"errors"
	"fmt"
	"gobang/common"
	applog "gobang/log"
	"gobang/pb"
	"gobang/server/config"
	"gobang/server/dao"
	"gobang/server/model"
	"gobang/server/service"
	"gobang/server/session"
	"gobang/server/tick"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

var (
	manager     = session.NewManager()
	store       *dao.Store
	authService *service.AuthService
	matchSvc    *service.MatchService
	botPool     *service.BotPool
)

func main() {
	cfg := config.GetConfigMgr()
	botPool = service.NewBotPool(cfg.BotPoolSize())

	if err := applog.Init(); err != nil {
		fmt.Println("初始化日志失败：", err)
		return
	}
	defer applog.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var err error
	store, err = dao.NewStore(ctx, cfg.MongoURI(), cfg.MongoDB())
	if err != nil {
		applog.Servicef("MongoDB 连接失败：%v", err)
		return
	}
	if err := store.EnsureIndexes(ctx); err != nil {
		applog.Servicef("MongoDB 索引初始化失败：%v", err)
		return
	}
	applog.Servicef("MongoDB 连接成功，数据库：%s", cfg.MongoDB())
	defer store.Disconnect(context.Background())

	authService = service.NewAuthService(store.Users)
	matchSvc = service.NewMatchService(manager, store.Games, botPool)
	botTickCtx, stopBotTick := context.WithCancel(context.Background())
	defer stopBotTick()
	go tick.NewBotTicker(manager, playBotTurn).Start(botTickCtx)
	go startWebServer()

	listener, err := net.Listen("tcp", cfg.TCPAddr())
	if err != nil {
		applog.Servicef("TCP 监听失败：%v", err)
		return
	}
	defer listener.Close()
	applog.Servicef("TCP Protobuf 服务启动：%s", cfg.TCPAddr())

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		applog.Servicef("服务端开始关闭")
		stopBotTick()
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
			return common.SendMsg(conn, msg)
		}
		applog.Servicef("TCP 客户端接入：%s", conn.RemoteAddr())
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
		msg, err := common.ReadMsg(player.Conn)
		if err != nil {
			applog.Servicef("TCP 玩家离线：user_id=%d username=%s", player.UserID, player.Username)
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
		applog.Servicef("注册失败：username=%s err=%v", msg.Username, err)
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_REGISTER_RESP, Tip: err.Error()})
		return
	}
	if err := manager.Authenticate(player, user.UserID, user.Username); err != nil {
		applog.Servicef("注册后登录失败：user_id=%d username=%s err=%v", user.UserID, user.Username, err)
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_REGISTER_RESP, Tip: err.Error()})
		return
	}
	applog.Servicef("玩家注册成功并登录：user_id=%d username=%s", user.UserID, user.Username)
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
		applog.Servicef("登录失败：username=%s err=%v", msg.Username, err)
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_LOGIN_RESP, Tip: err.Error()})
		return
	}
	if err := manager.Authenticate(player, user.UserID, user.Username); err != nil {
		applog.Servicef("登录拒绝：user_id=%d username=%s err=%v", user.UserID, user.Username, err)
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_LOGIN_RESP, Tip: err.Error()})
		return
	}
	applog.Servicef("玩家登录成功：user_id=%d username=%s", user.UserID, user.Username)
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
	if msg.AiVsAi {
		startAIVsAIGame(player)
		return
	}
	if msg.VsBot {
		applog.Servicef("玩家请求人机对战：user_id=%d username=%s", player.UserID, player.Username)
		game, err := matchSvc.JoinBot(player)
		if err != nil {
			applog.Servicef("人机对战创建失败：user_id=%d err=%v", player.UserID, err)
			sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_TIP, Tip: err.Error()})
			return
		}
		logGameStart(game)
		sendToPlayer(game.Black, service.StartMsg(pb.ChessType_BLACK, game.Black.UserID))
		return
	}
	game, matched, err := matchSvc.Join(player)
	if err != nil {
		applog.Servicef("玩家匹配失败：user_id=%d err=%v", player.UserID, err)
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_TIP, Tip: "匹配失败: " + err.Error()})
		return
	}
	if !matched {
		applog.Servicef("玩家进入匹配队列：user_id=%d username=%s", player.UserID, player.Username)
		sendToPlayer(player, &pb.GameMsg{MsgType: pb.MsgType_MSG_WAIT, Tip: "已进入匹配队列，等待对手..."})
		return
	}
	logGameStart(game)
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
	var draw bool
	var tip string

	game.Mu.Lock()
	if game.Finished {
		tip = "对局已结束"
	} else if game.Current != selfChess {
		tip = "未到你的回合，请等待对手"
	} else if x < 0 || x >= common.Size || y < 0 || y >= common.Size || game.Board[y][x] != pb.ChessType_EMPTY {
		tip = "落子位置非法，重新输入"
	} else {
		game.Board[y][x] = selfChess
		win = isWinLocked(game, int(x), int(y), selfChess)
		if win {
			game.Finished = true
		} else if isBoardFullLocked(game) {
			draw = true
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
	logMove(game, player.UserID, selfChess, x, y)

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
		logGameEnd(game, "胜负", winnerID, loserID)
		go persistGameResult(game.DBID, winnerID, loserID)
		return
	}
	if draw {
		broadcastGame(game, &pb.GameMsg{MsgType: pb.MsgType_MSG_DRAW, Tip: "棋盘已满，本局平局"})
		finishGame(game)
		logGameEnd(game, "平局", 0, 0)
		go persistGameDraw(game.DBID)
		return
	}
}

func handleQuitGame(player *session.PlayerSession) {
	if player.State == session.StateMatching {
		manager.CancelMatching(player)
		applog.Servicef("玩家退出匹配队列：user_id=%d username=%s", player.UserID, player.Username)
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
	logGameEnd(game, "主动退出", winnerID, player.UserID)
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
	logGameEnd(game, "断线退出", winnerID, leaver.UserID)
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

	var board [common.Size][common.Size]pb.ChessType
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
	var draw bool
	game.Mu.Lock()
	if !game.Finished && game.Current == bot.Chess && x >= 0 && x < common.Size && y >= 0 && y < common.Size && game.Board[y][x] == pb.ChessType_EMPTY {
		game.Board[y][x] = bot.Chess
		win = isWinLocked(game, int(x), int(y), bot.Chess)
		if win {
			game.Finished = true
		} else if isBoardFullLocked(game) {
			draw = true
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
	logMove(game, bot.UserID, bot.Chess, x, y)
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
		logGameEnd(game, "胜负", winnerID, loserID)
		go persistGameResult(game.DBID, winnerID, loserID)
		return
	}
	if draw {
		broadcastGame(game, &pb.GameMsg{MsgType: pb.MsgType_MSG_DRAW, Tip: "棋盘已满，本局平局"})
		finishGame(game)
		logGameEnd(game, "平局", 0, 0)
		go persistGameDraw(game.DBID)
	}
}

func startAIVsAIGame(watcher *session.PlayerSession) {
	aiClient, err := service.NewOpenAIClientFromEnv()
	if err != nil {
		sendToPlayer(watcher, &pb.GameMsg{MsgType: pb.MsgType_MSG_TIP, Tip: err.Error()})
		return
	}
	applog.Servicef("AI 对战使用 OpenAI 代理地址：%s", aiClient.BaseURL())
	black := &session.PlayerSession{UserID: -101, Username: "AI_BLACK", State: session.StateAuthenticated, IsBot: true}
	white := &session.PlayerSession{UserID: -102, Username: "AI_WHITE", State: session.StateAuthenticated, IsBot: true}
	ctx, cancel := dao.TimeoutContext()
	defer cancel()
	gameID, err := store.Games.CreateOngoing(ctx, black.UserID, white.UserID)
	if err != nil {
		sendToPlayer(watcher, &pb.GameMsg{MsgType: pb.MsgType_MSG_TIP, Tip: "创建 AI 对战记录失败：" + err.Error()})
		return
	}
	game := manager.CreateAIGame(black, white, watcher, gameID)
	logGameStart(game)
	modelName := aiClient.Model()
	sendToPlayer(watcher, &pb.GameMsg{
		MsgType: pb.MsgType_MSG_START,
		Chess:   pb.ChessType_BLACK,
		Tip:     fmt.Sprintf("AI 对战\n黑方模型：%s\n白方模型：%s", modelName, modelName),
	})
	go runAIVsAILoop(game, aiClient)
}

func runAIVsAILoop(game *session.GameContext, aiClient *service.OpenAIClient) {
	var moves []model.Move
	last := "-"
	for turn := 1; turn <= common.Size*common.Size; turn++ {
		game.Mu.Lock()
		if game.Finished {
			game.Mu.Unlock()
			return
		}
		current := game.Current
		board := game.Board
		game.Mu.Unlock()

		player := service.OpenAIPlayer{Color: current, PromptFile: "prompts/black_agent.txt"}
		if current == pb.ChessType_WHITE {
			player.PromptFile = "prompts/white_agent.txt"
		}
		myMoves, oppMoves := splitMoves(moves, current)
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		x, y, err := aiClient.NextMove(ctx, player, service.AIMoveContext{
			MyMoves:  myMoves,
			OppMoves: oppMoves,
			Turn:     turn,
			Last:     last,
		})
		cancel()
		if err != nil {
			sendToPlayer(game.Watcher, &pb.GameMsg{MsgType: pb.MsgType_MSG_TIP, Tip: "AI 调用失败：" + err.Error()})
			finishGame(game)
			logGameEnd(game, "AI调用失败", 0, 0)
			go persistGameDraw(game.DBID)
			return
		}
		if x < 0 || x >= common.Size || y < 0 || y >= common.Size || board[y][x] != pb.ChessType_EMPTY {
			fx, fy, ok := service.BestBotMove(board, current)
			if !ok {
				broadcastGame(game, &pb.GameMsg{MsgType: pb.MsgType_MSG_DRAW, Tip: "棋盘已满，本局平局"})
				finishGame(game)
				logGameEnd(game, "平局", 0, 0)
				go persistGameDraw(game.DBID)
				return
			}
			x, y = fx, fy
		}
		if applyAIMove(game, current, x, y, &moves) {
			return
		}
		last = fmt.Sprintf("(%d,%d)", x, y)
		time.Sleep(350 * time.Millisecond)
	}
}

func applyAIMove(game *session.GameContext, chess pb.ChessType, x, y int32, moves *[]model.Move) bool {
	userID := game.BlackID
	if chess == pb.ChessType_WHITE {
		userID = game.WhiteID
	}
	var win, draw bool
	game.Mu.Lock()
	if game.Finished || game.Current != chess || x < 0 || x >= common.Size || y < 0 || y >= common.Size || game.Board[y][x] != pb.ChessType_EMPTY {
		game.Mu.Unlock()
		return false
	}
	game.Board[y][x] = chess
	win = isWinLocked(game, int(x), int(y), chess)
	if win {
		game.Finished = true
	} else if isBoardFullLocked(game) {
		draw = true
		game.Finished = true
	} else if chess == pb.ChessType_BLACK {
		game.Current = pb.ChessType_WHITE
	} else {
		game.Current = pb.ChessType_BLACK
	}
	game.Mu.Unlock()

	putMsg := &pb.GameMsg{MsgType: pb.MsgType_MSG_PUT, Chess: chess, X: x, Y: y, UserId: userID}
	broadcastGame(game, putMsg)
	logMove(game, userID, chess, x, y)
	move := model.Move{UserID: userID, Chess: int32(chess), X: x, Y: y, At: time.Now()}
	*moves = append(*moves, move)
	go func() {
		ctx, cancel := dao.TimeoutContext()
		defer cancel()
		_ = store.Games.AddMove(ctx, game.DBID, move)
	}()
	if win {
		loserID := game.WhiteID
		if chess == pb.ChessType_WHITE {
			loserID = game.BlackID
		}
		broadcastGame(game, &pb.GameMsg{MsgType: pb.MsgType_MSG_WIN, Chess: chess, UserId: userID, Tip: "AI 对战结束，" + chess.String() + " 获胜"})
		finishGame(game)
		logGameEnd(game, "胜负", userID, loserID)
		go persistGameResult(game.DBID, userID, loserID)
		return true
	}
	if draw {
		broadcastGame(game, &pb.GameMsg{MsgType: pb.MsgType_MSG_DRAW, Tip: "棋盘已满，本局平局"})
		finishGame(game)
		logGameEnd(game, "平局", 0, 0)
		go persistGameDraw(game.DBID)
		return true
	}
	return false
}

func splitMoves(moves []model.Move, chess pb.ChessType) (string, string) {
	var mine []string
	var opp []string
	for _, move := range moves {
		item := fmt.Sprintf("(%d,%d)", move.X, move.Y)
		if pb.ChessType(move.Chess) == chess {
			mine = append(mine, item)
		} else {
			opp = append(opp, item)
		}
	}
	return strings.Join(mine, ","), strings.Join(opp, ",")
}

func sendToPlayer(player *session.PlayerSession, msg *pb.GameMsg) {
	if player == nil || player.IsBot {
		return
	}
	if player.Send != nil {
		logPush(player, msg)
		_ = player.Send(msg)
		return
	}
	if player.Conn != nil {
		logPush(player, msg)
		_ = common.SendMsg(player.Conn, msg)
	}
}

func broadcastGame(game *session.GameContext, msg *pb.GameMsg) {
	sendToPlayer(game.Black, msg)
	sendToPlayer(game.White, msg)
	sendToPlayer(game.Watcher, msg)
}

func logGameStart(game *session.GameContext) {
	mode := "玩家对战"
	if game.VsBot {
		mode = "人机对战"
	}
	applog.GameLine("")
	applog.GameLine("============================================================")
	applog.Gamef("对局开始：game_id=%s mode=%s black_id=%d black_name=%s white_id=%d white_name=%s",
		game.ID, mode, game.BlackID, playerName(game.Black), game.WhiteID, playerName(game.White))
}

func logMove(game *session.GameContext, userID int32, chess pb.ChessType, x, y int32) {
	applog.Gamef("对局落子：user_id=%d chess=%s x=%d y=%d", userID, chess.String(), x, y)
}

func logGameEnd(game *session.GameContext, reason string, winnerID, loserID int32) {
	applog.Gamef("对局结束：game_id=%s reason=%s black_id=%d white_id=%d winner_id=%d loser_id=%d",
		game.ID, reason, game.BlackID, game.WhiteID, winnerID, loserID)
	applog.GameLine("============================================================")
}

func logPush(player *session.PlayerSession, msg *pb.GameMsg) {
	if isGamePush(msg.MsgType) {
		return
	}
	applog.Servicef("服务端推送：to_user_id=%d to_username=%s msg_type=%s chess=%s x=%d y=%d tip=%s",
		player.UserID, player.Username, msg.MsgType.String(), msg.Chess.String(), msg.X, msg.Y, msg.Tip)
}

func isGamePush(msgType pb.MsgType) bool {
	return msgType == pb.MsgType_MSG_START ||
		msgType == pb.MsgType_MSG_PUT ||
		msgType == pb.MsgType_MSG_WIN ||
		msgType == pb.MsgType_MSG_DRAW
}

func playerName(player *session.PlayerSession) string {
	if player == nil {
		return ""
	}
	return player.Username
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

func persistGameDraw(gameID bson.ObjectID) {
	ctx, cancel := dao.TimeoutContext()
	defer cancel()
	_ = store.Games.Draw(ctx, gameID)
}

func isWinLocked(game *session.GameContext, x, y int, chess pb.ChessType) bool {
	dirs := [][]int{{1, 0}, {0, 1}, {1, 1}, {1, -1}}
	for _, d := range dirs {
		cnt := 1
		dx, dy := d[0], d[1]
		tmpx, tmpy := x+dx, y+dy
		for tmpx >= 0 && tmpx < common.Size && tmpy >= 0 && tmpy < common.Size && game.Board[tmpy][tmpx] == chess {
			cnt++
			tmpx += dx
			tmpy += dy
		}
		tmpx, tmpy = x-dx, y-dy
		for tmpx >= 0 && tmpx < common.Size && tmpy >= 0 && tmpy < common.Size && game.Board[tmpy][tmpx] == chess {
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

func isBoardFullLocked(game *session.GameContext) bool {
	for y := 0; y < common.Size; y++ {
		for x := 0; x < common.Size; x++ {
			if game.Board[y][x] == pb.ChessType_EMPTY {
				return false
			}
		}
	}
	return true
}
