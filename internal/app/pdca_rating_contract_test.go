package app

import (
	"encoding/json"
	"testing"
)

func TestPDCAJSONRatingPreservation(t *testing.T) {
	for _, tc := range []struct {
		name, rating string
		accepted     bool
	}{
		{"integer_one", "1", true},
		{"integer_five", "5", true},
		{"decimal", "5.0", false},
		{"exponent", "5e0", false},
		{"null", "null", false},
		{"boolean", "true", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var request generateRequest
			err := json.Unmarshal([]byte(`{"review":{"reviewText":"合成の口コミです","rating":`+tc.rating+`}}`), &request)
			accepted := false
			if err == nil {
				_, accepted = validReview(request)
			}
			if accepted != tc.accepted {
				t.Fatalf("accepted=%v, expected=%v", accepted, tc.accepted)
			}
		})
	}
}
