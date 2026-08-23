package httpx

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
)

func GetJSON(ctx context.Context, client *http.Client, url string, out any) error {
	return get(ctx, client, url, func(resp *http.Response) error {
		return json.NewDecoder(resp.Body).Decode(out)
	})
}

func GetXML(ctx context.Context, client *http.Client, url string, out any) error {
	return get(ctx, client, url, func(resp *http.Response) error {
		return xml.NewDecoder(resp.Body).Decode(out)
	})
}

func get(ctx context.Context, client *http.Client, url string, decode func(*http.Response) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	return decode(resp)
}
