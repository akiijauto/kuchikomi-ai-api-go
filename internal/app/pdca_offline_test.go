package app

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"testing"
)

type pdcaDeniedHTTP struct{}

func (pdcaDeniedHTTP) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("PDCA HTTP denied")
}

func TestMain(m *testing.M) {
	if os.Getenv("PDCA_OFFLINE") == "1" {
		http.DefaultTransport = pdcaDeniedHTTP{}
		http.DefaultClient = &http.Client{Transport: pdcaDeniedHTTP{}}
		if _, err := http.Get("https://example.invalid"); err == nil {
			panic("HTTP guard failed")
		}
		fmt.Println("PDCA HTTP guard verified")
	}
	os.Exit(m.Run())
}
