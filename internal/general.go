package internal

import (
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

var (
	Log        = logrus.New()
	httpClient = http.Client{
		Timeout: 5 * time.Second,
		Transport: &customTransport{
			roundTripper: &http.Transport{},
		},
	}
)

type customTransport struct {
	roundTripper http.RoundTripper
}

func (transport *customTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request.Header.Add("User-Agent", "S85.Berlin/1.0")
	return transport.roundTripper.RoundTrip(request)
}
