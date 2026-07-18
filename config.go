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
	defaultMaxMessageSize = 512

	defaultReadBufferSize  = 1024
	defaultWriteBufferSize = 1024

	defaultClientSendBuffer = 256
	defaultBroadcastBuffer  = 1024
	defaultRegisterBuffer   = 256
	defaultUnregisterBuffer = 256
	defaultStatusBuffer     = 256

	defaultBroadcastWorkers = 4
	defaultStatusWorkers    = 2

	defaultPersistBuffer        = 1024
	defaultPersistWorkers       = 1
	defaultPersistBatchSize     = 64
	defaultPersistFlushInterval = 5 * time.Millisecond

	defaultMaxUnreadMessages = 200
	defaultUnreadBatchSize   = 50
	defaultMarkReadBatch     = 500

	defaultSessionValidationInterval = 30 * time.Second
)

type HubConfig struct {
	RegisterBuffer   int
	UnregisterBuffer int
	BroadcastBuffer  int
	StatusBuffer     int
	ClientSendBuffer int

	BroadcastWorkers int
	StatusWorkers    int

	PersistBuffer        int
	PersistWorkers       int
	PersistBatchSize     int
	PersistFlushInterval time.Duration

	MaxUnreadMessages int
	UnreadBatchSize   int
	MarkReadBatch     int

	WriteWait      time.Duration
	PongWait       time.Duration
	PingPeriod     time.Duration
	MaxMessageSize int64

	ReadBufferSize      int
	WriteBufferSize     int
	CheckOrigin         func(*http.Request) bool
	ClientIDFromRequest func(*http.Request) (string, error)

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
		RegisterBuffer:       defaultRegisterBuffer,
		UnregisterBuffer:     defaultUnregisterBuffer,
		BroadcastBuffer:      defaultBroadcastBuffer,
		StatusBuffer:         defaultStatusBuffer,
		ClientSendBuffer:     defaultClientSendBuffer,
		BroadcastWorkers:     broadcastWorkers,
		StatusWorkers:        statusWorkers,
		PersistBuffer:        defaultPersistBuffer,
		PersistWorkers:       defaultPersistWorkers,
		PersistBatchSize:     defaultPersistBatchSize,
		PersistFlushInterval: defaultPersistFlushInterval,
		MaxUnreadMessages:    defaultMaxUnreadMessages,
		UnreadBatchSize:      defaultUnreadBatchSize,
		MarkReadBatch:        defaultMarkReadBatch,
		WriteWait:            defaultWriteWait,
		PongWait:             defaultPongWait,
		PingPeriod:           (defaultPongWait * 9) / 10,
		MaxMessageSize:       defaultMaxMessageSize,
		ReadBufferSize:       defaultReadBufferSize,
		WriteBufferSize:      defaultWriteBufferSize,
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
	if cfg.BroadcastWorkers <= 0 {
		cfg.BroadcastWorkers = def.BroadcastWorkers
	}
	if cfg.StatusWorkers <= 0 {
		cfg.StatusWorkers = def.StatusWorkers
	}
	if cfg.PersistBuffer <= 0 {
		cfg.PersistBuffer = def.PersistBuffer
	}
	if cfg.PersistWorkers <= 0 {
		cfg.PersistWorkers = def.PersistWorkers
	}
	if cfg.PersistBatchSize <= 0 {
		cfg.PersistBatchSize = def.PersistBatchSize
	}
	if cfg.PersistFlushInterval <= 0 {
		cfg.PersistFlushInterval = def.PersistFlushInterval
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
	if cfg.ClientIDFromRequest == nil {
		cfg.ClientIDFromRequest = def.ClientIDFromRequest
	}
	if cfg.ValidateClientSession != nil && cfg.SessionValidationInterval <= 0 {
		cfg.SessionValidationInterval = defaultSessionValidationInterval
	}
	return cfg
}
