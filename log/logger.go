package applog

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

var (
	serviceLogger *log.Logger
	gameLogger    *log.Logger
	serviceFile   *os.File
	gameFile      *os.File
)

func Init() error {
	dir := logDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	var err error
	serviceFile, err = os.OpenFile(filepath.Join(dir, "server.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	gameFile, err = os.OpenFile(filepath.Join(dir, "game.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		_ = serviceFile.Close()
		return err
	}

	serviceLogger = log.New(io.MultiWriter(os.Stdout, serviceFile), "", 0)
	gameLogger = log.New(io.MultiWriter(os.Stdout, gameFile), "", 0)
	return nil
}

func Close() {
	if serviceFile != nil {
		_ = serviceFile.Close()
	}
	if gameFile != nil {
		_ = gameFile.Close()
	}
}

func Servicef(format string, args ...any) {
	write(serviceLogger, format, args...)
}

func Gamef(format string, args ...any) {
	write(gameLogger, format, args...)
}

func GameLine(text string) {
	if gameLogger == nil {
		fmt.Println(text)
		return
	}
	gameLogger.Print(text)
}

func write(logger *log.Logger, format string, args ...any) {
	if logger == nil {
		fmt.Printf(format+"\n", args...)
		return
	}
	logger.Printf("%s "+format, append([]any{time.Now().Format("2006-01-02 15:04:05")}, args...)...)
}

func logDir() string {
	if _, err := os.Stat("web/index.html"); err == nil {
		return "log"
	}
	return "../log"
}
