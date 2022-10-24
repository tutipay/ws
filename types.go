package chat

import (
	"encoding/json"
)

// Response is the object returned by the api in all cases.
type Response struct {
	// Status will be null if we're sending chat messages
	Status string `json:"status,omitempty"`

	// Messages will be null if we're sending status of a user
	Messages []Message `json:"messages,omitempty"`
}

type validationError struct {
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
}

func marshal(o any) []byte {
	d, _ := json.Marshal(&o)
	return d
}
