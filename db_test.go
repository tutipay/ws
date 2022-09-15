package chat

import (
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

var testDb, _ = openDb()

func TestChat_insert(t *testing.T) {

	tests := []struct {
		name    string
		fields  Message
		wantErr bool
	}{
		{"test insertion", Message{To: "091212121", From: "404304343", Text: "This is just a test"}, false},
		{"test insertion", Message{To: "091212121", From: "494343", Text: "second message"}, false},
		{"test insertion", Message{To: "32", From: "4843", Text: "second message"}, false},
		{"test insertion", Message{To: "323232", From: "494343", Text: "second message"}, false},
		{"test insertion", Message{To: "wqwq", From: "494343", Text: "second message"}, false},
		{"test insertion", Message{To: "32", From: "494343", Text: "second message"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := insert(tt.fields, testDb); err != nil {
				t.Errorf("Chat.insert() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestChat_getUnreadMessages(t *testing.T) {

	tests := []struct {
		name   string
		fields Message
		args   string
		want   int
	}{
		{"test retrieving", Message{}, "091212121", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getUnreadMessages(tt.args, testDb)
			if (err != nil) && len(got) != tt.want {
				t.Errorf("Chat.getUnreadMessages() error = %v, length %v", err, tt.want)
				return
			}
		})
	}
}

func TestMessage_readAll(t *testing.T) {
	type args struct {
		mobile string
		db     *sqlx.DB
	}
	tests := []struct {
		name string
		args args
	}{
		{"make isDelivered receipt", args{mobile: "091212121", db: testDb}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := updateStatus(tt.args.mobile, tt.args.db); err != nil {
				t.Errorf("Message.readAll() error = %v", err)
			}
		})
	}
}
