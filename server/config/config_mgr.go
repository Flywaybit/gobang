package config

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

const (
	defaultTCPAddr     = "0.0.0.0:8888"
	defaultWEBAddr     = "0.0.0.0:8889"
	defaultMongoURI    = "mongodb://127.0.0.1:27017"
	defaultMongoDB     = "gobang"
	defaultRedisAddr   = "127.0.0.1:6379"
	defaultBotPoolSize = 10
	defaultLogDir      = "log"
	defaultOpenAIModel = "gpt-5.5"
	defaultOpenAIBase  = "https://outllm.chaoziran.com"
)

type ConfigMgr struct {
	tcpAddr       string
	webAddr       string
	mongoURI      string
	mongoDB       string
	redisAddr     string
	botPoolSize   int
	logDir        string
	openAIAPIKey  string
	openAIModel   string
	openAIBaseURL string
}

var (
	configOnce sync.Once
	configMgr  *ConfigMgr
)

func GetConfigMgr() *ConfigMgr {
	configOnce.Do(func() {
		configMgr = &ConfigMgr{
			tcpAddr:       getString("TCP_ADDR", defaultTCPAddr),
			webAddr:       getString("WEB_ADDR", defaultWEBAddr),
			mongoURI:      getString("MONGO_URI", defaultMongoURI),
			mongoDB:       getString("MONGO_DB", defaultMongoDB),
			redisAddr:     getString("REDIS_ADDR", defaultRedisAddr),
			botPoolSize:   getInt("BOT_POOL_SIZE", defaultBotPoolSize),
			logDir:        getString("LOG_DIR", defaultLogDir),
			openAIAPIKey:  getString("OPENAI_API_KEY", ""),
			openAIModel:   getString("OPENAI_MODEL", defaultOpenAIModel),
			openAIBaseURL: normalizeOpenAIBaseURL(getString("OPENAI_BASE_URL", defaultOpenAIBase)),
		}
	})
	return configMgr
}

func (c *ConfigMgr) TCPAddr() string {
	return c.tcpAddr
}

func (c *ConfigMgr) WEBAddr() string {
	return c.webAddr
}

func (c *ConfigMgr) WebAddr() string {
	return c.webAddr
}

func (c *ConfigMgr) MongoURI() string {
	return c.mongoURI
}

func (c *ConfigMgr) MongoDB() string {
	return c.mongoDB
}

func (c *ConfigMgr) RedisAddr() string {
	return c.redisAddr
}

func (c *ConfigMgr) BotPoolSize() int {
	return c.botPoolSize
}

func (c *ConfigMgr) LogDir() string {
	return c.logDir
}

func (c *ConfigMgr) OpenAIAPIKey() string {
	return c.openAIAPIKey
}

func (c *ConfigMgr) OpenAIModel() string {
	return c.openAIModel
}

func (c *ConfigMgr) OpenAIBaseURL() string {
	return c.openAIBaseURL
}

func getString(name, defaultValue string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return defaultValue
	}
	return value
}

func getInt(name string, defaultValue int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return defaultValue
	}
	return parsed
}

func normalizeOpenAIBaseURL(value string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return defaultOpenAIBase + "/v1"
	}
	if !strings.HasSuffix(value, "/v1") {
		value += "/v1"
	}
	return value
}
