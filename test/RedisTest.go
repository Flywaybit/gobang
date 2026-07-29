//go:build redistest

package main

import (
	"bufio"
	"fmt"
	"gobang/server/config"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

func main() {
	addr := config.GetConfigMgr().RedisAddr()
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		panic(fmt.Sprintf("connect Redis failed, addr=%s err=%v", addr, err))
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	if err := redisCmd(conn, reader, "PING").expectSimple("PONG"); err != nil {
		panic(err)
	}
	fmt.Println("Redis PING ok")

	key := "gobang:redis:test"
	value := fmt.Sprintf("redis-test-%d", time.Now().Unix())
	if err := redisCmd(conn, reader, "SET", key, value).expectSimple("OK"); err != nil {
		panic(err)
	}
	fmt.Println("Redis SET ok")

	got, err := redisCmd(conn, reader, "GET", key).bulkString()
	if err != nil {
		panic(err)
	}
	if got != value {
		panic(fmt.Sprintf("Redis GET mismatch, got=%q want=%q", got, value))
	}
	fmt.Println("Redis GET ok:", got)

	if n, err := redisCmd(conn, reader, "DEL", key).integer(); err != nil {
		panic(err)
	} else {
		fmt.Println("Redis DEL ok, deleted:", n)
	}

	fmt.Println("Redis test passed")
}

type redisReply struct {
	reader *bufio.Reader
	err    error
}

func redisCmd(conn net.Conn, reader *bufio.Reader, args ...string) redisReply {
	var builder strings.Builder
	builder.WriteString("*")
	builder.WriteString(strconv.Itoa(len(args)))
	builder.WriteString("\r\n")
	for _, arg := range args {
		builder.WriteString("$")
		builder.WriteString(strconv.Itoa(len(arg)))
		builder.WriteString("\r\n")
		builder.WriteString(arg)
		builder.WriteString("\r\n")
	}
	_, err := conn.Write([]byte(builder.String()))
	return redisReply{reader: reader, err: err}
}

func (r redisReply) expectSimple(want string) error {
	value, err := r.simpleString()
	if err != nil {
		return err
	}
	if value != want {
		return fmt.Errorf("unexpected Redis reply, got=%q want=%q", value, want)
	}
	return nil
}

func (r redisReply) simpleString() (string, error) {
	if r.err != nil {
		return "", r.err
	}
	line, err := r.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if strings.HasPrefix(line, "-") {
		return "", fmt.Errorf("Redis error: %s", strings.TrimPrefix(line, "-"))
	}
	if !strings.HasPrefix(line, "+") {
		return "", fmt.Errorf("unexpected Redis simple string reply: %q", line)
	}
	return strings.TrimPrefix(line, "+"), nil
}

func (r redisReply) bulkString() (string, error) {
	if r.err != nil {
		return "", r.err
	}
	line, err := r.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if strings.HasPrefix(line, "-") {
		return "", fmt.Errorf("Redis error: %s", strings.TrimPrefix(line, "-"))
	}
	if !strings.HasPrefix(line, "$") {
		return "", fmt.Errorf("unexpected Redis bulk string reply: %q", line)
	}
	size, err := strconv.Atoi(strings.TrimPrefix(line, "$"))
	if err != nil {
		return "", err
	}
	if size < 0 {
		return "", fmt.Errorf("Redis returned nil bulk string")
	}
	buf := make([]byte, size+2)
	if _, err := io.ReadFull(r.reader, buf); err != nil {
		return "", err
	}
	return string(buf[:size]), nil
}

func (r redisReply) integer() (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	line, err := r.reader.ReadString('\n')
	if err != nil {
		return 0, err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if strings.HasPrefix(line, "-") {
		return 0, fmt.Errorf("Redis error: %s", strings.TrimPrefix(line, "-"))
	}
	if !strings.HasPrefix(line, ":") {
		return 0, fmt.Errorf("unexpected Redis integer reply: %q", line)
	}
	return strconv.Atoi(strings.TrimPrefix(line, ":"))
}
