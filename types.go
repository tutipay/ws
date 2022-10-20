package chat

import (
	"encoding/json"
)

// Response is the object returned by the api in all cases.
type Response struct {
	// Type is the type of Response, there are mainly 2 types:
	// 1. "status": which is used in notifying the users that one of their contacts is online.
	// 2. "chat": which is used in sending regular chat messages.
	Type string

	// Message is the actual message and it depends on the Type of Response.
	// 1. In case of "status", its type will be string, and its value will be the ID (phone number) of the client that became online.
	// 2. In case of "chat", its type is []Message, and its value will be the actual chat messages.
	Message any
}

type validationError struct {
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
}

func marshal(o any) []byte {
	d, _ := json.Marshal(&o)
	return d
}
