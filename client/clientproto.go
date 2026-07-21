package main

import (
	"bufio"
	"fmt"
	"gobang/pb"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

type ClientState int

const (
	StatusLogin ClientState = iota
	StatusAuthPending
	StatusMainMenu
	StatusMatching
	StatusPlaying
	StatusGameOver
)

type ClientApp struct {
	mu           sync.RWMutex
	currentState ClientState
	localBoard   [Size][Size]pb.ChessType
	selfChess    pb.ChessType
	userID       int32
	username     string
	lastResult   string
	changed      chan struct{}
	loginStep    int
	loginAction  string
	loginName    string
}

func main() {
	conn, err := net.Dial("tcp", "127.0.0.1:8888")
	if err != nil {
		fmt.Println("连接服务器失败:", err)
		return
	}
	defer conn.Close()
	fmt.Println("成功连接服务器")

	app := &ClientApp{currentState: StatusLogin, changed: make(chan struct{}, 1)}
	go recvLoop(conn, app)
	sendLoop(conn, app)
}

func recvLoop(conn net.Conn, app *ClientApp) {
	for {
		msg, err := readMsg(conn)
		if err != nil {
			fmt.Println("\n服务器断开，游戏退出")
			os.Exit(0)
		}
		switch msg.MsgType {
		case pb.MsgType_MSG_LOGIN_RESP, pb.MsgType_MSG_REGISTER_RESP:
			if msg.UserId > 0 {
				app.mu.Lock()
				app.userID = msg.UserId
				app.username = msg.Username
				app.currentState = StatusMainMenu
				app.loginStep = 0
				app.mu.Unlock()
				app.notify()
				fmt.Println("\n" + msg.Tip)
			} else {
				app.setState(StatusLogin)
				fmt.Println("\n认证失败：", msg.Tip)
			}
		case pb.MsgType_MSG_WAIT:
			app.setState(StatusMatching)
			fmt.Println("\n【等待】", msg.Tip)
		case pb.MsgType_MSG_START:
			app.mu.Lock()
			app.selfChess = msg.Chess
			app.currentState = StatusPlaying
			app.localBoard = [Size][Size]pb.ChessType{}
			app.lastResult = ""
			app.mu.Unlock()
			app.notify()
			clearScreen()
			fmt.Println("===== 对局开始 =====")
			fmt.Println("提示：", msg.Tip)
			printBoard(app)
		case pb.MsgType_MSG_PUT:
			app.mu.Lock()
			if msg.Y >= 0 && msg.Y < Size && msg.X >= 0 && msg.X < Size {
				app.localBoard[msg.Y][msg.X] = msg.Chess
			}
			app.mu.Unlock()
			clearScreen()
			printBoard(app)
		case pb.MsgType_MSG_WIN:
			app.mu.Lock()
			app.currentState = StatusGameOver
			app.lastResult = msg.Tip
			app.localBoard = [Size][Size]pb.ChessType{}
			app.mu.Unlock()
			app.notify()
			fmt.Println("\n===== 对局结算 =====")
			fmt.Println(msg.Tip)
		case pb.MsgType_MSG_DRAW:
			app.mu.Lock()
			app.currentState = StatusGameOver
			app.lastResult = msg.Tip
			app.localBoard = [Size][Size]pb.ChessType{}
			app.mu.Unlock()
			app.notify()
			fmt.Println("\n===== 对局结算 =====")
			fmt.Println(msg.Tip)
		case pb.MsgType_MSG_TIP:
			fmt.Println("\n系统提示：", msg.Tip)
		}
	}
}

func sendLoop(conn net.Conn, app *ClientApp) {
	scan := bufio.NewScanner(os.Stdin)
	inputCh := make(chan string)
	go func() {
		for scan.Scan() {
			inputCh <- strings.TrimSpace(scan.Text())
		}
		close(inputCh)
	}()

	lastPrompt := ""
	for {
		state := app.state()
		key := app.promptKey(state)
		if key != lastPrompt {
			printPrompt(app, state)
			lastPrompt = key
		}

		select {
		case <-app.changed:
		case input, ok := <-inputCh:
			if !ok {
				return
			}
			handleInput(conn, app, input)
		}
	}
}

func printPrompt(app *ClientApp, state ClientState) {
	switch state {
	case StatusLogin:
		switch app.loginStepState() {
		case 0:
			fmt.Print("\n请选择：1 登录，2 注册：")
		case 1:
			fmt.Print("用户名：")
		case 2:
			fmt.Print("密码：")
		}
	case StatusAuthPending:
		fmt.Println("请求已发送，等待服务器响应...")
	case StatusMainMenu:
		fmt.Println("\n===== 主菜单 =====")
		fmt.Println("A. 匹配对战（玩家 vs 玩家）")
		fmt.Println("B. 人机对战")
		fmt.Print("请选择：")
	case StatusMatching:
		fmt.Println("匹配中，请等待...")
	case StatusPlaying:
		fmt.Print("\n输入落子坐标 x y（空格分隔，0-14）：")
	case StatusGameOver:
		fmt.Println("\n1. 继续匹配下一局")
		fmt.Println("2. 退出房间（回到主菜单）")
		fmt.Print("请选择：")
	}
}

func handleInput(conn net.Conn, app *ClientApp, input string) {
	switch app.state() {
	case StatusLogin:
		handleLoginInput(conn, app, input)
	case StatusMainMenu:
		handleMainMenuInput(conn, app, input)
	case StatusPlaying:
		handlePlayingInput(conn, app, input)
	case StatusGameOver:
		handleGameOverInput(conn, app, input)
	}
}

func handleLoginInput(conn net.Conn, app *ClientApp, input string) {
	switch app.loginStepState() {
	case 0:
		if input != "1" && input != "2" {
			fmt.Println("输入错误，请输入 1 或 2")
			return
		}
		app.setLoginStep(1, input, "")
	case 1:
		if input == "" {
			fmt.Println("用户名不能为空")
			return
		}
		app.setLoginStep(2, "", input)
	case 2:
		if input == "" {
			fmt.Println("密码不能为空")
			return
		}
		action, username := app.loginDraft()
		app.resetLoginDraft()
		sendAuth(conn, app, action, username, input)
	}
}

func sendAuth(conn net.Conn, app *ClientApp, action, username, password string) {
	msgType := pb.MsgType_MSG_LOGIN_REQ
	if action == "2" {
		msgType = pb.MsgType_MSG_REGISTER_REQ
	}
	app.setState(StatusAuthPending)
	_ = sendMsg(conn, &pb.GameMsg{
		MsgType:  msgType,
		Username: username,
		Password: password,
	})
}

func handleMainMenuInput(conn net.Conn, app *ClientApp, input string) {
	choice := strings.ToUpper(strings.TrimSpace(input))
	switch choice {
	case "A":
		app.setState(StatusMatching)
		_, userID := app.identity()
		_ = sendMsg(conn, &pb.GameMsg{MsgType: pb.MsgType_MSG_MATCH_REQ, UserId: userID})
	case "B":
		app.setState(StatusMatching)
		_, userID := app.identity()
		_ = sendMsg(conn, &pb.GameMsg{MsgType: pb.MsgType_MSG_MATCH_REQ, UserId: userID, VsBot: true})
	default:
		fmt.Println("输入错误，请输入 A 或 B")
	}
}

func handlePlayingInput(conn net.Conn, app *ClientApp, input string) {
	if app.state() != StatusPlaying {
		return
	}
	if strings.EqualFold(input, "quit") {
		_ = sendMsg(conn, &pb.GameMsg{MsgType: pb.MsgType_MSG_QUIT_GAME})
		app.setState(StatusMainMenu)
		return
	}
	parts := strings.Fields(input)
	if len(parts) != 2 {
		fmt.Println("输入错误，示例：7 7；输入 quit 可退出当前对局")
		return
	}
	x, err1 := strconv.Atoi(parts[0])
	y, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		fmt.Println("请输入数字坐标")
		return
	}
	chess, userID := app.identity()
	_ = sendMsg(conn, &pb.GameMsg{
		MsgType: pb.MsgType_MSG_PUT,
		Chess:   chess,
		X:       int32(x),
		Y:       int32(y),
		UserId:  userID,
	})
}

func handleGameOverInput(conn net.Conn, app *ClientApp, input string) {
	choice := strings.TrimSpace(input)
	switch choice {
	case "1":
		app.setState(StatusMatching)
		_, userID := app.identity()
		_ = sendMsg(conn, &pb.GameMsg{MsgType: pb.MsgType_MSG_MATCH_REQ, UserId: userID})
	case "2":
		app.setState(StatusMainMenu)
		_ = sendMsg(conn, &pb.GameMsg{MsgType: pb.MsgType_MSG_QUIT_GAME})
	default:
		fmt.Println("输入错误，请输入 1 或 2")
	}
}

func (a *ClientApp) state() ClientState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.currentState
}

func (a *ClientApp) promptKey(state ClientState) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return fmt.Sprintf("%d:%d", state, a.loginStep)
}

func (a *ClientApp) setState(state ClientState) {
	a.mu.Lock()
	a.currentState = state
	a.mu.Unlock()
	a.notify()
}

func (a *ClientApp) notify() {
	select {
	case a.changed <- struct{}{}:
	default:
	}
}

func (a *ClientApp) identity() (pb.ChessType, int32) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.selfChess, a.userID
}

func (a *ClientApp) loginStepState() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.loginStep
}

func (a *ClientApp) setLoginStep(step int, action, name string) {
	a.mu.Lock()
	a.loginStep = step
	if action != "" {
		a.loginAction = action
	}
	if name != "" {
		a.loginName = name
	}
	a.mu.Unlock()
	a.notify()
}

func (a *ClientApp) loginDraft() (string, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.loginAction, a.loginName
}

func (a *ClientApp) resetLoginDraft() {
	a.mu.Lock()
	a.loginStep = 0
	a.loginAction = ""
	a.loginName = ""
	a.mu.Unlock()
}

func clearScreen() {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "cls")
	} else {
		cmd = exec.Command("clear")
	}
	cmd.Stdout = os.Stdout
	_ = cmd.Run()
}

func printBoard(app *ClientApp) {
	app.mu.RLock()
	board := app.localBoard
	chess := app.selfChess
	app.mu.RUnlock()

	fmt.Println("\n    0  1  2  3  4  5  6  7  8  9 10 11 12 13 14")
	for y := 0; y < Size; y++ {
		fmt.Printf("%2d", y)
		for x := 0; x < Size; x++ {
			switch board[y][x] {
			case pb.ChessType_EMPTY:
				fmt.Print("  ·")
			case pb.ChessType_BLACK:
				fmt.Print("  ●")
			case pb.ChessType_WHITE:
				fmt.Print("  ○")
			}
		}
		fmt.Println()
	}
	if chess == pb.ChessType_BLACK {
		fmt.Println("你执黑棋 ●")
	} else {
		fmt.Println("你执白棋 ○")
	}
	fmt.Println("输入 quit 可主动退出当前对局")
}
