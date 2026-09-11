package httpx

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const maxBody = 64 << 20

type StatusError struct {
	URL    string
	Status int
}

func (e StatusError) Error() string {
	return fmt.Sprintf("GET %s: status %d", e.URL, e.Status)
}

func (e StatusError) NoSuchThing() bool {
	return e.Status == http.StatusBadRequest || e.Status == http.StatusNotFound
}

func NothingThere(err error) bool {
	var status StatusError
	return errors.As(err, &status) && status.NoSuchThing()
}

func GetJSON(ctx context.Context, client *http.Client, url string, out any) error {
	return GetJSONWithHeaders(ctx, client, url, nil, out)
}

func GetJSONWithHeaders(ctx context.Context, client *http.Client, url string, headers map[string]string, out any) error {
	return get(ctx, client, url, headers, func(resp *http.Response) error {
		return json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out)
	})
}

func GetXML(ctx context.Context, client *http.Client, url string, out any) error {
	return get(ctx, client, url, nil, func(resp *http.Response) error {
		return xml.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out)
	})
}

func get(ctx context.Context, client *http.Client, url string, headers map[string]string, decode func(*http.Response) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return StatusError{URL: url, Status: resp.StatusCode}
	}
	return decode(resp)
}
