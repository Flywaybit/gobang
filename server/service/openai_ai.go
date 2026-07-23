package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gobang/pb"
	"gobang/server/config"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type OpenAIPlayer struct {
	Color      pb.ChessType
	PromptFile string
}

type AIMoveContext struct {
	MyMoves  string
	OppMoves string
	Turn     int
	Last     string
}

type OpenAIClient struct {
	apiKey  string
	model   string
	baseURL string
	cache   *PromptCache
	http    *http.Client
}

func (c *OpenAIClient) BaseURL() string {
	return c.baseURL
}

func (c *OpenAIClient) Model() string {
	return c.model
}

type PromptCache struct {
	mu    sync.Mutex
	items map[string]promptItem
}

type promptItem struct {
	modTime time.Time
	text    string
}

type aiMove struct {
	X int `json:"x"`
	Y int `json:"y"`
}

func NewOpenAIClientFromEnv() (*OpenAIClient, error) {
	cfg := config.GetConfigMgr()
	apiKey := cfg.OpenAIAPIKey()
	if apiKey == "" {
		return nil, errors.New("未配置 OPENAI_API_KEY，无法启动 AI 对战")
	}
	return &OpenAIClient{
		apiKey:  apiKey,
		model:   cfg.OpenAIModel(),
		baseURL: cfg.OpenAIBaseURL(),
		cache:   &PromptCache{items: make(map[string]promptItem)},
		http:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *OpenAIClient) NextMove(ctx context.Context, player OpenAIPlayer, vars AIMoveContext) (int32, int32, error) {
	prompt, err := c.cache.Render(player.PromptFile, map[string]string{
		"COLOR":     player.Color.String(),
		"MY_MOVES":  vars.MyMoves,
		"OPP_MOVES": vars.OppMoves,
		"TURN":      fmt.Sprintf("%d", vars.Turn),
		"LAST":      vars.Last,
	})
	if err != nil {
		return 0, 0, err
	}

	body := map[string]any{
		"model": c.model,
		"input": prompt,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return 0, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, 0, fmt.Errorf("OpenAI 调用失败：status=%d body=%s", resp.StatusCode, string(respBody))
	}
	text := extractOutputText(respBody)
	move, err := parseAIMove(text)
	if err != nil {
		return 0, 0, err
	}
	return int32(move.X), int32(move.Y), nil
}

func (c *PromptCache) Render(path string, values map[string]string) (string, error) {
	text, err := c.load(path)
	if err != nil {
		return "", err
	}
	for key, value := range values {
		text = strings.ReplaceAll(text, "{"+key+"}", value)
	}
	return text, nil
}

func (c *PromptCache) load(path string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	resolved := promptPath(path)
	stat, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	item, ok := c.items[resolved]
	if ok && item.modTime.Equal(stat.ModTime()) {
		return item.text, nil
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", err
	}
	text := string(data)
	c.items[resolved] = promptItem{modTime: stat.ModTime(), text: text}
	return text, nil
}

func promptPath(path string) string {
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return filepath.Join("..", path)
}

func extractOutputText(data []byte) string {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return string(data)
	}
	if value, ok := raw["output_text"].(string); ok {
		return value
	}
	output, _ := raw["output"].([]any)
	var parts []string
	for _, item := range output {
		obj, _ := item.(map[string]any)
		content, _ := obj["content"].([]any)
		for _, c := range content {
			part, _ := c.(map[string]any)
			if text, ok := part["text"].(string); ok {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func parseAIMove(text string) (aiMove, error) {
	var move aiMove
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &move); err == nil {
		return move, nil
	}
	re := regexp.MustCompile(`-?\d+`)
	nums := re.FindAllString(text, -1)
	if len(nums) < 2 {
		return move, fmt.Errorf("AI 返回中未找到坐标：%s", text)
	}
	_, _ = fmt.Sscanf(nums[0]+" "+nums[1], "%d %d", &move.X, &move.Y)
	return move, nil
}
