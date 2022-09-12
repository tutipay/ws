package chat

import "time"

type Message struct {
	text        []byte
	from        string // Client ID
	to          string // Clinet ID
	amount      float64
	isDelivered bool
	date        time.Time
}
