package app

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPDCARatingJSONType(t *testing.T) {
	for name, rating := range map[string]string{"one": `"1"`, "three": `"3"`, "five": `"5"`, "escaped": `"\u0035"`} {
		t.Run(name, func(t *testing.T) {
			var request generateRequest
			err := json.Unmarshal([]byte(`{"review":{"reviewText":"合成の口コミです","rating":`+rating+`}}`), &request)
			if err == nil {
				if _, accepted := validReview(request); accepted {
					t.Fatal("quoted JSON rating was accepted as an integer")
				}
			}
		})
	}
}

func TestPDCAQuotedRatingHTTP(t *testing.T) {
	defer func() {
		if recover() != nil {
			t.Error("invalid input reached a nil store instead of returning 400")
		}
	}()
	s := &Server{}
	request := httptest.NewRequest("POST", "/api/generate", strings.NewReader(`{"review":{"reviewText":"合成の口コミです","rating":"5"}}`))
	response := httptest.NewRecorder()
	s.handleGenerate(response, request, "synthetic-user")
	if response.Code != 400 || !strings.Contains(response.Body.String(), "入力内容を確認してください") {
		t.Fatalf("expected validation response, status=%d", response.Code)
	}
}
