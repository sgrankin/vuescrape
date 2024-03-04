package vueclient

import (
	"fmt"
	"net/http"

	"golang.org/x/time/rate"
)

// throttledTransport is an http.RoundTripper that waits on a built in rate.Limiter for each request.
type throttledTransport struct {
	Base    http.RoundTripper
	Limiter *rate.Limiter
}

func (t *throttledTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.Limiter.Wait(req.Context()); err != nil {
		return nil, fmt.Errorf("limiter: %w", err)
	}
	return t.Base.RoundTrip(req)
}
