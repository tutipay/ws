package chat

import "encoding/json"

type Room struct {
	ID         string
	senderID   string
	receiverID string
}

type validationError struct {
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
}

func marshal(o any) []byte {
	d, _ := json.Marshal(&o)
	return d
}
