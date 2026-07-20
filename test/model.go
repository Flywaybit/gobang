package main

// import "encoding/json"

// // 棋子常量
// const (
// 	Empty = 0  // 空
// 	Black = 1  // 黑棋，先手
// 	White = 2  // 白棋，后手
// 	Size  = 15 // 15*15棋盘
// )

// // 消息类型
// const (
// 	MsgPut   = 1 // 落子消息
// 	MsgWin   = 2 // 胜利通知
// 	MsgDraw  = 3 // 和棋
// 	MsgWait  = 4 // 等待对手
// 	MsgStart = 5 // 对局开始
// )

// // 通信消息结构体
// type Msg struct {
// 	Type  int    `json:"type"` // 消息类型
// 	X     int    `json:"x"`
// 	Y     int    `json:"y"`
// 	Chess int    `json:"chess"` // 当前落子方
// 	Tip   string `json:"tip"`
// }

// // 序列化为json字符串
// func (m *Msg) ToJson() string {
// 	data, _ := json.Marshal(m)
// 	return string(data) + "\n" // 换行做消息分隔符
// }

// // json转Msg
// func ParseMsg(str string) (*Msg, error) {
// 	var m Msg
// 	err := json.Unmarshal([]byte(str), &m)
// 	return &m, err
// }
