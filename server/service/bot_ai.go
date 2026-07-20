package service

import "gobang/pb"

const boardSize = 15

func BestBotMove(board [boardSize][boardSize]pb.ChessType, botChess pb.ChessType) (int32, int32, bool) {
	opponent := pb.ChessType_BLACK
	if botChess == pb.ChessType_BLACK {
		opponent = pb.ChessType_WHITE
	}

	bestX, bestY := int32(-1), int32(-1)
	bestScore := -1
	for y := 0; y < boardSize; y++ {
		for x := 0; x < boardSize; x++ {
			if board[y][x] != pb.ChessType_EMPTY {
				continue
			}
			score := scorePoint(board, x, y, botChess)*2 + scorePoint(board, x, y, opponent)
			if score > bestScore {
				bestScore = score
				bestX, bestY = int32(x), int32(y)
			}
		}
	}
	return bestX, bestY, bestScore >= 0
}

func scorePoint(board [boardSize][boardSize]pb.ChessType, x, y int, chess pb.ChessType) int {
	dirs := [][2]int{{1, 0}, {0, 1}, {1, 1}, {1, -1}}
	total := 0
	for _, d := range dirs {
		count := 1
		open := 0
		count += countDirection(board, x, y, d[0], d[1], chess, &open)
		count += countDirection(board, x, y, -d[0], -d[1], chess, &open)
		score := count * count * 10
		if open == 2 {
			score *= 2
		}
		if count >= 5 {
			score += 100000
		}
		total += score
	}
	centerBias := 7 - abs(x-7) + 7 - abs(y-7)
	return total + centerBias
}

func countDirection(board [boardSize][boardSize]pb.ChessType, x, y, dx, dy int, chess pb.ChessType, open *int) int {
	count := 0
	for {
		x += dx
		y += dy
		if x < 0 || x >= boardSize || y < 0 || y >= boardSize {
			return count
		}
		if board[y][x] == chess {
			count++
			continue
		}
		if board[y][x] == pb.ChessType_EMPTY {
			*open++
		}
		return count
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
