package vmclient

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"

	"golang.org/x/sync/errgroup"
)

// Client is a VictoriaMetrics client that can run simple queries and push data.
type Client struct {
	Dest url.URL
}

// Returns *Sample for scalars, []Series for vectors.
func (c *Client) Query(q string) (any, error) {
	rt, rv, err := c.query(q)
	if err != nil {
		return nil, err
	}
	log.Printf("result is %v %q", rt, rv)
	switch rt {
	case resultTypeScalar:
		var result Sample
		if err := json.Unmarshal(rv, &result); err != nil {
			return nil, err
		}
		return &result, nil
	case resultTypeVector:
		var result []struct {
			Metric Metric `json:"metric"`
			Value  Sample `json:"value"`
		}
		if err := json.Unmarshal(rv, &result); err != nil {
			return nil, err
		}
		log.Printf("unmarshaled: %+v", result)
		var out []Series
		for _, r := range result {
			out = append(out, Series{
				Metric:  r.Metric,
				Samples: []Sample{r.Value},
			})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("result type unsupported: %q %q", rt, rv)
	}
}

func (c *Client) query(q string) (resultType, json.RawMessage, error) {
	// TODO: add context and figure out cancelation.

	v := url.Values{}
	v.Set("query", q)

	u := c.Dest.JoinPath("/api/v1/query")
	u.RawQuery = v.Encode()

	rep, err := http.Get(u.String())
	if err != nil {
		return "", nil, fmt.Errorf("get: %w", err)
	}
	defer rep.Body.Close()
	if rep.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(rep.Body)
		return "", nil, fmt.Errorf("get failed with status %s: %s", rep.Status, body)
	}
	var body struct {
		Data struct {
			ResultType resultType      `json:"resultType"`
			Result     json.RawMessage `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rep.Body).Decode(&body); err != nil {
		return "", nil, err
	}
	return body.Data.ResultType, body.Data.Result, nil
}

type resultType string

const (
	// https://prometheus.io/docs/prometheus/latest/querying/api/#expression-query-result-formats
	resultTypeMatrix resultType = "matrix"
	resultTypeVector resultType = "vector"
	resultTypeScalar resultType = "scalar"
	resultTypeString resultType = "string"
)

func (c *Client) Push() (*Pusher, error) {
	// TODO: add context and figure out cancelation.
	r, w := io.Pipe()
	g := &errgroup.Group{}
	g.Go(func() error {
		req, err := http.NewRequest("POST", c.Dest.JoinPath("/api/v1/import").String(), r)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Encoding", "gzip")
		resp, err := http.DefaultClient.Do(req)
		if resp.StatusCode >= 400 {
			dump, err := httputil.DumpResponse(resp, true)
			if err != nil {
				log.Printf("request failed (%s);", resp.Status)
			} else {
				log.Printf("request failed (%s); response:\n%s", resp.Status, dump)
			}
		}
		return err
	})
	gzw := gzip.NewWriter(w)
	enc := json.NewEncoder(gzw)
	enc.SetIndent("", "")
	return &Pusher{g, w, gzw, enc}, nil
}

type Pusher struct {
	g   *errgroup.Group
	w   io.Closer
	gzw io.WriteCloser
	enc *json.Encoder
}

func (p *Pusher) Close() error {
	if p == nil {
		return nil
	}
	return errors.Join(
		p.gzw.Close(),
		p.w.Close(),
		p.g.Wait(),
	)
}

func (p *Pusher) Push(s *Series) error {
	return p.enc.Encode(s)
}
