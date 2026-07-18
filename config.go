package chat

import (
	"context"
	"net/http"
	"runtime"
	"time"
)

const (
	defaultWriteWait      = 10 * time.Second
	defaultPongWait       = 60 * time.Second
	defaultMaxMessageSize = 16 << 10

	defaultReadBufferSize  = 1024
	defaultWriteBufferSize = 1024

	defaultClientSendBuffer = 256
	defaultBroadcastBuffer  = 1024
	defaultStatusBuffer     = 256

	defaultBroadcastWorkers = 4
	defaultStatusWorkers    = 2

	defaultMaxUnreadMessages = 200
	defaultUnreadBatchSize   = 50
	defaultMarkReadBatch     = 500

	defaultSessionValidationInterval = 30 * time.Second
)

type HubConfig struct {
	BroadcastBuffer  int
	StatusBuffer     int
	ClientSendBuffer int

	BroadcastWorkers int
	StatusWorkers    int

	MaxUnreadMessages int
	UnreadBatchSize   int
	MarkReadBatch     int

	WriteWait      time.Duration
	PongWait       time.Duration
	PingPeriod     time.Duration
	MaxMessageSize int64

	ReadBufferSize            int
	WriteBufferSize           int
	CheckOrigin               func(*http.Request) bool
	ClientIdentityFromRequest func(*http.Request) (ClientIdentity, error)

	// ValidateClientSession is called before the websocket upgrade and
	// periodically for the lifetime of the connection. A validation error
	// rejects or closes the connection. Leaving it nil disables validation.
	ValidateClientSession     func(context.Context) error
	SessionValidationInterval time.Duration
}

func DefaultHubConfig() HubConfig {
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	broadcastWorkers := workers
	if broadcastWorkers < defaultBroadcastWorkers {
		broadcastWorkers = defaultBroadcastWorkers
	}
	statusWorkers := workers / 2
	if statusWorkers < 1 {
		statusWorkers = 1
	}
	return HubConfig{
		BroadcastBuffer:   defaultBroadcastBuffer,
		StatusBuffer:      defaultStatusBuffer,
		ClientSendBuffer:  defaultClientSendBuffer,
		BroadcastWorkers:  broadcastWorkers,
		StatusWorkers:     statusWorkers,
		MaxUnreadMessages: defaultMaxUnreadMessages,
		UnreadBatchSize:   defaultUnreadBatchSize,
		MarkReadBatch:     defaultMarkReadBatch,
		WriteWait:         defaultWriteWait,
		PongWait:          defaultPongWait,
		PingPeriod:        (defaultPongWait * 9) / 10,
		MaxMessageSize:    defaultMaxMessageSize,
		ReadBufferSize:    defaultReadBufferSize,
		WriteBufferSize:   defaultWriteBufferSize,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
}

func (cfg HubConfig) withDefaults() HubConfig {
	def := DefaultHubConfig()
	if cfg.BroadcastBuffer <= 0 {
		cfg.BroadcastBuffer = def.BroadcastBuffer
	}
	if cfg.StatusBuffer <= 0 {
		cfg.StatusBuffer = def.StatusBuffer
	}
	if cfg.ClientSendBuffer <= 0 {
		cfg.ClientSendBuffer = def.ClientSendBuffer
	}
	if cfg.BroadcastWorkers <= 0 {
		cfg.BroadcastWorkers = def.BroadcastWorkers
	}
	if cfg.StatusWorkers <= 0 {
		cfg.StatusWorkers = def.StatusWorkers
	}
	if cfg.MaxUnreadMessages <= 0 {
		cfg.MaxUnreadMessages = def.MaxUnreadMessages
	}
	if cfg.UnreadBatchSize <= 0 {
		cfg.UnreadBatchSize = def.UnreadBatchSize
	}
	if cfg.MarkReadBatch <= 0 {
		cfg.MarkReadBatch = def.MarkReadBatch
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
	if cfg.ValidateClientSession != nil && cfg.SessionValidationInterval <= 0 {
		cfg.SessionValidationInterval = defaultSessionValidationInterval
	}
	return cfg
}
