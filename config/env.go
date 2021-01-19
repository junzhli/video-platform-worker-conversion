package config

import (
	"os"
	"runtime"
	"strconv"
)

type EnvConfig struct {
	// Database
	DatabaseUsername string
	DatabasePassword string
	DatabaseServerAddress string
	DatabaseServerPort string
	// MessageQueue
	MessageQueuePassword string
	MessageQueueServerAddress string
	// FFmpeg path
	FFmpegProgramPath string
	MaxNumberOfWorkers string
}

func getOrDefault(expect string, def string) string {
	if len(expect) == 0 {
		return def
	}
	return expect
}

func ReadEnv() EnvConfig {
	return EnvConfig{
		DatabaseUsername: getOrDefault(os.Getenv("DB_USER"), ""),
		DatabasePassword: getOrDefault(os.Getenv("DB_PASS"), ""),
		DatabaseServerAddress: getOrDefault(os.Getenv("DB_SERVER"), "127.0.0.1"),
		DatabaseServerPort: getOrDefault(os.Getenv("DB_PORT"), "27017"),
		MessageQueueServerAddress: getOrDefault(os.Getenv("MQ_SERVER"), "127.0.0.1"),
		MessageQueuePassword: getOrDefault(os.Getenv("MQ_PASS"), ""),
		FFmpegProgramPath: getOrDefault(os.Getenv("FFMPEG_BIN"), ""),
		MaxNumberOfWorkers: getOrDefault(os.Getenv("WORKER_MAX_NUM"), strconv.Itoa(runtime.NumCPU())),
	}
}