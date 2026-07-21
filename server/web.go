package main

import (
	"fmt"
	"gobang/pb"
	"gobang/server/session"
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/websocket"
)

type WebMsg struct {
	MsgType  string `json:"msg_type"`
	Chess    string `json:"chess,omitempty"`
	X        int32  `json:"x,omitempty"`
	Y        int32  `json:"y,omitempty"`
	Tip      string `json:"tip,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	UserID   int32  `json:"user_id,omitempty"`
	VsBot    bool   `json:"vs_bot,omitempty"`
}

type wsPeer struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func startWebServer() {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(webDir())))
	mux.HandleFunc("/ws", handleWebSocket)
	fmt.Println("Web server started: http://127.0.0.1:8889")
	if err := http.ListenAndServe("127.0.0.1:8889", mux); err != nil {
		fmt.Println("Web server stopped:", err)
	}
}

func webDir() string {
	if _, err := os.Stat("../web/index.html"); err == nil {
		return "../web"
	}
	return "web"
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	peer := &wsPeer{conn: conn}
	player := manager.AddSession(func(msg *pb.GameMsg) error {
		peer.mu.Lock()
		defer peer.mu.Unlock()
		return peer.conn.WriteJSON(fromPB(msg))
	})

	defer func() {
		_, game := manager.RemoveSession(player)
		if game != nil {
			handleInterruptedGame(game, player)
		}
		_ = conn.Close()
	}()

	for {
		var webMsg WebMsg
		if err := conn.ReadJSON(&webMsg); err != nil {
			fmt.Println("Web player disconnected:", player.Username)
			return
		}
		handleGameMsg(player, toPB(webMsg))
	}
}

func toPB(msg WebMsg) *pb.GameMsg {
	return &pb.GameMsg{
		MsgType:  msgTypeFromString(msg.MsgType),
		Chess:    chessFromString(msg.Chess),
		X:        msg.X,
		Y:        msg.Y,
		Tip:      msg.Tip,
		Username: msg.Username,
		Password: msg.Password,
		UserId:   msg.UserID,
		VsBot:    msg.VsBot,
	}
}

func fromPB(msg *pb.GameMsg) WebMsg {
	return WebMsg{
		MsgType:  msg.MsgType.String(),
		Chess:    msg.Chess.String(),
		X:        msg.X,
		Y:        msg.Y,
		Tip:      msg.Tip,
		Username: msg.Username,
		UserID:   msg.UserId,
		VsBot:    msg.VsBot,
	}
}

func msgTypeFromString(value string) pb.MsgType {
	switch value {
	case "MSG_LOGIN_REQ":
		return pb.MsgType_MSG_LOGIN_REQ
	case "MSG_REGISTER_REQ":
		return pb.MsgType_MSG_REGISTER_REQ
	case "MSG_MATCH_REQ":
		return pb.MsgType_MSG_MATCH_REQ
	case "MSG_PUT":
		return pb.MsgType_MSG_PUT
	case "MSG_QUIT_GAME":
		return pb.MsgType_MSG_QUIT_GAME
	default:
		return pb.MsgType_MSG_TIP
	}
}

func chessFromString(value string) pb.ChessType {
	switch value {
	case "BLACK":
		return pb.ChessType_BLACK
	case "WHITE":
		return pb.ChessType_WHITE
	default:
		return pb.ChessType_EMPTY
	}
}

var _ *session.PlayerSession
