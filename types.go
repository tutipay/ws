package chat

import (
	"encoding/json"
)

// StatusResponse is the response send as part of Response in case we are sending
// a status (online/offline) message.
type StatusResponse struct {
	Mobile           string `json:"mobile,omitempty"`
	ConnectionStatus string `json:"connection_status,omitempty"`
}

// Response is the object returned by the api in all cases.
type Response struct {
	// Status will be null if we're sending chat messages
	Status StatusResponse `json:"status,omitempty"`

	// Messages will be null if we're sending status of a user
	Messages []Message `json:"messages,omitempty"`
}

type ContactsRequest struct {
	Name   string `json:"name,omitempty"`
	Mobile string `json:"mobile,omitempty"`
}

type User ContactsRequest

type validationError struct {
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
}

func marshal(o any) []byte {
	d, _ := json.Marshal(&o)
	return d
}
