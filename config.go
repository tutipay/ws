package chat

import (
	"net/http"
	"time"
)

const (
	defaultWriteWait      = 10 * time.Second
	defaultPongWait       = 60 * time.Second
	defaultMaxMessageSize = 512

	defaultReadBufferSize  = 1024
	defaultWriteBufferSize = 1024

	defaultClientSendBuffer = 256
	defaultBroadcastBuffer  = 1024
	defaultRegisterBuffer   = 256
	defaultUnregisterBuffer = 256
	defaultStatusBuffer     = 256
)

type HubConfig struct {
	RegisterBuffer   int
	UnregisterBuffer int
	BroadcastBuffer  int
	StatusBuffer     int
	ClientSendBuffer int

	WriteWait      time.Duration
	PongWait       time.Duration
	PingPeriod     time.Duration
	MaxMessageSize int64

	ReadBufferSize  int
	WriteBufferSize int
	CheckOrigin     func(*http.Request) bool
}

func DefaultHubConfig() HubConfig {
	return HubConfig{
		RegisterBuffer:   defaultRegisterBuffer,
		UnregisterBuffer: defaultUnregisterBuffer,
		BroadcastBuffer:  defaultBroadcastBuffer,
		StatusBuffer:     defaultStatusBuffer,
		ClientSendBuffer: defaultClientSendBuffer,
		WriteWait:        defaultWriteWait,
		PongWait:         defaultPongWait,
		PingPeriod:       (defaultPongWait * 9) / 10,
		MaxMessageSize:   defaultMaxMessageSize,
		ReadBufferSize:   defaultReadBufferSize,
		WriteBufferSize:  defaultWriteBufferSize,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
}

func (cfg HubConfig) withDefaults() HubConfig {
	def := DefaultHubConfig()
	if cfg.RegisterBuffer <= 0 {
		cfg.RegisterBuffer = def.RegisterBuffer
	}
	if cfg.UnregisterBuffer <= 0 {
		cfg.UnregisterBuffer = def.UnregisterBuffer
	}
	if cfg.BroadcastBuffer <= 0 {
		cfg.BroadcastBuffer = def.BroadcastBuffer
	}
	if cfg.StatusBuffer <= 0 {
		cfg.StatusBuffer = def.StatusBuffer
	}
	if cfg.ClientSendBuffer <= 0 {
		cfg.ClientSendBuffer = def.ClientSendBuffer
	}
	if cfg.WriteWait <= 0 {
		cfg.WriteWait = def.WriteWait
	}
	if cfg.PongWait <= 0 {
		cfg.PongWait = def.PongWait
	}
	if cfg.PingPeriod <= 0 {
		cfg.PingPeriod = (cfg.PongWait * 9) / 10
	}
	if cfg.MaxMessageSize <= 0 {
		cfg.MaxMessageSize = def.MaxMessageSize
	}
	if cfg.ReadBufferSize <= 0 {
		cfg.ReadBufferSize = def.ReadBufferSize
	}
	if cfg.WriteBufferSize <= 0 {
		cfg.WriteBufferSize = def.WriteBufferSize
	}
	if cfg.CheckOrigin == nil {
		cfg.CheckOrigin = def.CheckOrigin
	}
	return cfg
}
