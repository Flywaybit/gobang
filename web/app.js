const Status = {
  Auth: "auth",
  Menu: "menu",
  Matching: "matching",
  Playing: "playing",
  GameOver: "game_over",
};

const state = {
  ws: null,
  status: Status.Auth,
  mode: "login",
  userId: 0,
  username: "",
  chess: "EMPTY",
  matchVsBot: false,
  board: Array.from({ length: 15 }, () => Array(15).fill("EMPTY")),
};

const $ = (id) => document.getElementById(id);

function connect() {
  state.ws = new WebSocket(`ws://${location.host}/ws`);
  state.ws.onmessage = (event) => handleMessage(JSON.parse(event.data));
  state.ws.onclose = () => {
    state.status = Status.Auth;
    $("authTip").textContent = "连接已断开，请刷新页面";
    show("authPanel");
    syncControls();
  };
}

function send(msg) {
  if (!state.ws || state.ws.readyState !== WebSocket.OPEN) return;
  state.ws.send(JSON.stringify(msg));
}

function handleMessage(msg) {
  switch (msg.msg_type) {
    case "MSG_LOGIN_RESP":
    case "MSG_REGISTER_RESP":
      if (msg.user_id > 0) {
        state.userId = msg.user_id;
        state.username = msg.username;
        state.status = Status.Menu;
        $("welcome").textContent = `主菜单 - ${state.username}`;
        $("authTip").textContent = msg.tip || "";
        $("menuTip").textContent = "";
        show("menuPanel");
      } else {
        state.status = Status.Auth;
        $("authTip").textContent = msg.tip || "认证失败";
      }
      break;
    case "MSG_WAIT":
      if (state.status === Status.Matching) {
        $("matchingTip").textContent = msg.tip || "正在寻找对手...";
      }
      break;
    case "MSG_START":
      state.status = Status.Playing;
      state.chess = msg.chess;
      resetBoard();
      $("resultModal").classList.add("hidden");
      $("gamePanel").classList.remove("locked");
      $("gameTitle").textContent = state.chess === "BLACK" ? "你执黑棋" : "你执白棋";
      $("gameTip").textContent = msg.tip || "";
      show("gamePanel");
      renderBoard();
      break;
    case "MSG_PUT":
      if (state.status === Status.Playing && msg.y >= 0 && msg.y < 15 && msg.x >= 0 && msg.x < 15) {
        state.board[msg.y][msg.x] = msg.chess;
        renderBoard();
      }
      break;
    case "MSG_WIN":
      state.status = Status.GameOver;
      $("resultText").textContent = msg.tip || "对局结束";
      $("gamePanel").classList.add("locked");
      $("resultModal").classList.remove("hidden");
      break;
    case "MSG_TIP":
      handleTip(msg.tip || "");
      break;
  }
  syncControls();
}

function handleTip(tip) {
  if (state.status === Status.Matching && tip.includes("退出匹配")) {
    state.status = Status.Menu;
    $("menuTip").textContent = tip;
    show("menuPanel");
    return;
  }
  if (state.status === Status.Matching) {
    $("matchingTip").textContent = tip;
  } else if (state.status === Status.Playing || state.status === Status.GameOver) {
    $("gameTip").textContent = tip;
  } else {
    $("menuTip").textContent = tip;
  }
}

function show(id) {
  ["authPanel", "menuPanel", "matchingPanel", "gamePanel"].forEach((panel) => $(panel).classList.add("hidden"));
  $(id).classList.remove("hidden");
}

function syncControls() {
  const isMenu = state.status === Status.Menu;
  const isMatching = state.status === Status.Matching;
  const isPlaying = state.status === Status.Playing;
  $("pvpBtn").disabled = !isMenu;
  $("botBtn").disabled = !isMenu;
  $("cancelMatchBtn").disabled = !isMatching;
  $("quitBtn").disabled = !isPlaying;
}

function startMatch(vsBot) {
  if (state.status !== Status.Menu) return;
  state.status = Status.Matching;
  state.matchVsBot = vsBot;
  $("matchingTip").textContent = vsBot ? "正在创建人机对局..." : "正在寻找对手...";
  show("matchingPanel");
  syncControls();
  send({ msg_type: "MSG_MATCH_REQ", user_id: state.userId, vs_bot: vsBot });
}

function cancelMatch() {
  if (state.status !== Status.Matching) return;
  send({ msg_type: "MSG_QUIT_GAME" });
  state.status = Status.Menu;
  $("menuTip").textContent = "已退出匹配队列";
  show("menuPanel");
  syncControls();
}

function resetBoard() {
  state.board = Array.from({ length: 15 }, () => Array(15).fill("EMPTY"));
}

function renderBoard() {
  const board = $("board");
  board.innerHTML = "";
  [[3, 3], [11, 3], [7, 7], [3, 11], [11, 11]].forEach(([x, y]) => {
    const star = document.createElement("span");
    star.className = "star";
    star.style.left = pointPercent(x);
    star.style.top = pointPercent(y);
    board.appendChild(star);
  });
  for (let y = 0; y < 15; y++) {
    for (let x = 0; x < 15; x++) {
      const cell = document.createElement("button");
      cell.className = "cell";
      cell.style.left = pointPercent(x);
      cell.style.top = pointPercent(y);
      if (state.board[y][x] === "BLACK") cell.classList.add("black");
      if (state.board[y][x] === "WHITE") cell.classList.add("white");
      cell.onclick = () => {
        if (state.status !== Status.Playing) return;
        send({ msg_type: "MSG_PUT", x, y, chess: state.chess, user_id: state.userId });
      };
      board.appendChild(cell);
    }
  }
}

function pointPercent(index) {
  return `${7 + (index / 14) * 86}%`;
}

$("loginTab").onclick = () => {
  if (state.status !== Status.Auth) return;
  state.mode = "login";
  $("loginTab").classList.add("active");
  $("registerTab").classList.remove("active");
  $("authBtn").textContent = "登录";
};

$("registerTab").onclick = () => {
  if (state.status !== Status.Auth) return;
  state.mode = "register";
  $("registerTab").classList.add("active");
  $("loginTab").classList.remove("active");
  $("authBtn").textContent = "注册";
};

$("authBtn").onclick = () => {
  if (state.status !== Status.Auth) return;
  send({
    msg_type: state.mode === "login" ? "MSG_LOGIN_REQ" : "MSG_REGISTER_REQ",
    username: $("username").value.trim(),
    password: $("password").value,
  });
};

$("pvpBtn").onclick = () => startMatch(false);
$("botBtn").onclick = () => startMatch(true);
$("cancelMatchBtn").onclick = cancelMatch;

$("quitBtn").onclick = () => {
  if (state.status !== Status.Playing) return;
  send({ msg_type: "MSG_QUIT_GAME" });
};

$("nextBtn").onclick = () => {
  if (state.status !== Status.GameOver) return;
  $("resultModal").classList.add("hidden");
  $("gamePanel").classList.remove("locked");
  state.status = Status.Menu;
  show("menuPanel");
  syncControls();
  startMatch(false);
};

$("backBtn").onclick = () => {
  if (state.status !== Status.GameOver) return;
  $("resultModal").classList.add("hidden");
  $("gamePanel").classList.remove("locked");
  state.status = Status.Menu;
  show("menuPanel");
  syncControls();
};

connect();
renderBoard();
syncControls();
