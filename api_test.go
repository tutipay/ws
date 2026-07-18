package chat

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubmitContacts_AcceptsOnlyBoundedResolvedUserIDs(t *testing.T) {
	db := newTestDB(t)
	owner := testIdentity("tenant", 1)

	request := httptest.NewRequest(http.MethodPost, "/contacts", bytes.NewBufferString(`[{"user_id":2},{"user_id":2}]`))
	response := httptest.NewRecorder()
	SubmitContacts(owner, db, response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("valid contacts status=%d body=%s", response.Code, response.Body.String())
	}
	var count int
	if err := db.Get(&count, `SELECT count(*) FROM contacts_v2`); err != nil || count != 1 {
		t.Fatalf("valid contact count=%d error=%v", count, err)
	}

	tests := map[string]string{
		"empty":         `[]`,
		"self only":     `[{"user_id":1}]`,
		"invalid id":    `[{"user_id":0}]`,
		"unknown field": `[{"user_id":2,"mobile":"0990000000"}]`,
	}
	tooMany := make([]ContactsRequest, maxContactsPerRequest+1)
	for index := range tooMany {
		tooMany[index].UserID = int64(index + 2)
	}
	encoded, err := json.Marshal(tooMany)
	if err != nil {
		t.Fatalf("marshal oversized contacts: %v", err)
	}
	tests["too many"] = string(encoded)

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/contacts", bytes.NewBufferString(body))
			response := httptest.NewRecorder()
			SubmitContacts(owner, db, response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
