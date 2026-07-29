package session

import (
	"errors"
	"gobang/pb"
	"net"
	"sync"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type State int

const (
	StateUnauthenticated State = iota
	StateAuthenticated
	StateMatching
	StatePlaying
)

type PlayerSession struct {
	Conn              net.Conn
	Send              func(*pb.GameMsg) error
	UserID            int32
	Username          string
	State             State
	GameID            string
	Chess             pb.ChessType
	IsBot             bool
	StopOnlineRefresh func()
}

type GameContext struct {
	ID       string
	DBID     bson.ObjectID
	BlackID  int32
	WhiteID  int32
	Black    *PlayerSession
	White    *PlayerSession
	Board    [15][15]pb.ChessType
	Current  pb.ChessType
	Finished bool
	VsBot    bool
	AIVsAI   bool
	Watcher  *PlayerSession
	Mu       sync.Mutex
}

type Manager struct {
	mu       sync.Mutex
	players  map[net.Conn]*PlayerSession
	sessions map[*PlayerSession]struct{}
	byUserID map[int32]*PlayerSession
	games    map[string]*GameContext
	queue    []*PlayerSession
	nextGame int64
}

func NewManager() *Manager {
	return &Manager{
		players:  make(map[net.Conn]*PlayerSession),
		sessions: make(map[*PlayerSession]struct{}),
		byUserID: make(map[int32]*PlayerSession),
		games:    make(map[string]*GameContext),
	}
}

func (m *Manager) AddConn(conn net.Conn) *PlayerSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &PlayerSession{Conn: conn, State: StateUnauthenticated}
	m.players[conn] = s
	m.sessions[s] = struct{}{}
	return s
}

func (m *Manager) AddSession(send func(*pb.GameMsg) error) *PlayerSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &PlayerSession{Send: send, State: StateUnauthenticated}
	m.sessions[s] = struct{}{}
	return s
}

var ErrUserOnline = errors.New("用户已在其他客户端登录")

func (m *Manager) Authenticate(s *PlayerSession, userID int32, username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.byUserID[userID]; existing != nil && existing != s {
		return ErrUserOnline
	}
	if s.UserID != 0 && s.UserID != userID {
		delete(m.byUserID, s.UserID)
	}
	s.UserID = userID
	s.Username = username
	s.State = StateAuthenticated
	m.byUserID[userID] = s
	return nil
}

func (m *Manager) ClearAuthentication(s *PlayerSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s == nil {
		return
	}
	if s.UserID != 0 && m.byUserID[s.UserID] == s {
		delete(m.byUserID, s.UserID)
	}
	s.UserID = 0
	s.Username = ""
	s.State = StateUnauthenticated
}

func (m *Manager) GetByConn(conn net.Conn) *PlayerSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.players[conn]
}

func (m *Manager) Enqueue(s *PlayerSession) *PlayerSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.State == StateMatching || s.State == StatePlaying {
		return nil
	}
	for _, queued := range m.queue {
		if queued.UserID == s.UserID {
			s.State = StateMatching
			return nil
		}
	}
	s.State = StateMatching
	if len(m.queue) == 0 {
		m.queue = append(m.queue, s)
		return nil
	}
	opponent := m.queue[0]
	m.queue = m.queue[1:]
	if opponent == s || opponent.State != StateMatching || opponent.UserID == s.UserID {
		m.queue = append(m.queue, s)
		return nil
	}
	return opponent
}

func (m *Manager) CancelMatching(s *PlayerSession) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.State != StateMatching {
		return false
	}
	for i, queued := range m.queue {
		if queued == s {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			s.State = StateAuthenticated
			return true
		}
	}
	s.State = StateAuthenticated
	return true
}

func (m *Manager) CreateGame(black, white *PlayerSession, dbID bson.ObjectID) *GameContext {
	return m.CreateGameWithMode(black, white, dbID, false)
}

func (m *Manager) CreateGameWithMode(black, white *PlayerSession, dbID bson.ObjectID, vsBot bool) *GameContext {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextGame++
	id := dbID.Hex()
	if id == "" {
		id = string(rune(m.nextGame))
	}
	game := &GameContext{
		ID:      id,
		DBID:    dbID,
		BlackID: black.UserID,
		WhiteID: white.UserID,
		Black:   black,
		White:   white,
		Current: pb.ChessType_BLACK,
		VsBot:   vsBot,
	}
	black.State = StatePlaying
	white.State = StatePlaying
	black.GameID = id
	white.GameID = id
	black.Chess = pb.ChessType_BLACK
	white.Chess = pb.ChessType_WHITE
	m.games[id] = game
	return game
}

func (m *Manager) CreateAIGame(black, white, watcher *PlayerSession, dbID bson.ObjectID) *GameContext {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := dbID.Hex()
	game := &GameContext{
		ID:      id,
		DBID:    dbID,
		BlackID: black.UserID,
		WhiteID: white.UserID,
		Black:   black,
		White:   white,
		Current: pb.ChessType_BLACK,
		AIVsAI:  true,
		Watcher: watcher,
	}
	black.State = StatePlaying
	white.State = StatePlaying
	black.GameID = id
	white.GameID = id
	black.Chess = pb.ChessType_BLACK
	white.Chess = pb.ChessType_WHITE
	if watcher != nil {
		watcher.State = StatePlaying
		watcher.GameID = id
	}
	m.games[id] = game
	return game
}

func (m *Manager) GetGame(id string) *GameContext {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.games[id]
}

func (m *Manager) RemoveGame(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.games, id)
}

func (m *Manager) EndGame(game *GameContext) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if game.Black != nil {
		game.Black.State = StateAuthenticated
		game.Black.GameID = ""
	}
	if game.White != nil && !game.White.IsBot {
		game.White.State = StateAuthenticated
		game.White.GameID = ""
	}
	if game.Watcher != nil {
		game.Watcher.State = StateAuthenticated
		game.Watcher.GameID = ""
	}
	delete(m.games, game.ID)
}

func (m *Manager) FinishGame(game *GameContext) {
	m.mu.Lock()
	defer m.mu.Unlock()
	game.Finished = true
	if game.Black != nil {
		game.Black.State = StateAuthenticated
		game.Black.GameID = ""
	}
	if game.White != nil && !game.White.IsBot {
		game.White.State = StateAuthenticated
		game.White.GameID = ""
	}
	delete(m.games, game.ID)
}

func (m *Manager) RemoveConn(conn net.Conn) (*PlayerSession, *GameContext) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.players[conn]
	if s == nil {
		return nil, nil
	}
	delete(m.players, conn)
	delete(m.sessions, s)
	if s.UserID != 0 {
		delete(m.byUserID, s.UserID)
	}
	for i, queued := range m.queue {
		if queued == s {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			break
		}
	}
	var game *GameContext
	if s.GameID != "" {
		game = m.games[s.GameID]
		delete(m.games, s.GameID)
	}
	return s, game
}

func (m *Manager) RemoveSession(s *PlayerSession) (*PlayerSession, *GameContext) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[s]; !ok {
		return nil, nil
	}
	delete(m.sessions, s)
	if s.Conn != nil {
		delete(m.players, s.Conn)
	}
	if s.UserID != 0 {
		delete(m.byUserID, s.UserID)
	}
	for i, queued := range m.queue {
		if queued == s {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			break
		}
	}
	var game *GameContext
	if s.GameID != "" {
		game = m.games[s.GameID]
		delete(m.games, s.GameID)
	}
	return s, game
}

func (m *Manager) ActiveGames() []*GameContext {
	m.mu.Lock()
	defer m.mu.Unlock()
	games := make([]*GameContext, 0, len(m.games))
	for _, game := range m.games {
		games = append(games, game)
	}
	return games
}

func (m *Manager) ActiveSessions() []*PlayerSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	sessions := make([]*PlayerSession, 0, len(m.sessions))
	for s := range m.sessions {
		sessions = append(sessions, s)
	}
	return sessions
}

func (g *GameContext) Opponent(s *PlayerSession) *PlayerSession {
	if s == g.Black {
		return g.White
	}
	return g.Black
}

func (g *GameContext) UserIDByChess(chess pb.ChessType) int32 {
	if chess == pb.ChessType_BLACK {
		return g.BlackID
	}
	return g.WhiteID
}
