package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

type Client struct {
	BaseURL string
	Secret  string
	client  *http.Client
}

func NewClient(baseURL, secret string) *Client {
	return &Client{
		BaseURL: "http://" + baseURL,
		Secret:  secret,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

type ProxyDelay struct {
	Name  string
	Delay int
	Error string
}

func (c *Client) TestAllProxies(timeout int, testURL string) ([]ProxyDelay, error) {
	names, err := c.getProxyNames()
	if err != nil {
		return nil, fmt.Errorf("get proxies: %w", err)
	}

	results := make([]ProxyDelay, 0, len(names))
	for _, name := range names {
		delay, err := c.testDelay(name, timeout, testURL)
		d := ProxyDelay{Name: name}
		if err != nil {
			d.Error = err.Error()
		} else {
			d.Delay = delay
		}
		results = append(results, d)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Error != results[j].Error {
			return results[i].Error < results[j].Error
		}
		return results[i].Delay < results[j].Delay
	})

	return results, nil
}

func (c *Client) getProxyNames() ([]string, error) {
	body, err := c.doGet("/proxies")
	if err != nil {
		return nil, err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse proxies: %w", err)
	}

	// The API wraps proxies under a "proxies" key
	proxiesRaw, ok := raw["proxies"]
	if !ok {
		return nil, fmt.Errorf("unexpected API response format")
	}

	var proxies map[string]json.RawMessage
	if err := json.Unmarshal(proxiesRaw, &proxies); err != nil {
		return nil, fmt.Errorf("parse proxies map: %w", err)
	}

	groupTypes := map[string]bool{
		"Selector": true, "URLTest": true, "Fallback": true, "LoadBalance": true,
	}

	var names []string
	for name, raw := range proxies {
		if name == "__GLOBAL__" {
			continue
		}
		var proxy struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &proxy); err != nil {
			continue
		}
		if !groupTypes[proxy.Type] {
			names = append(names, name)
		}
	}

	return names, nil
}

func (c *Client) testDelay(name string, timeout int, testURL string) (int, error) {
	url := fmt.Sprintf("%s/proxies/%s/delay?timeout=%d&url=%s", c.BaseURL, name, timeout, testURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	if c.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("timeout")
	}

	var result struct {
		Delay int `json:"delay"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("parse response")
	}

	return result.Delay, nil
}

type GroupInfo struct {
	Name string
	Type string
	Now  string
	All  []string
}

func (c *Client) ListGroups() ([]GroupInfo, error) {
	body, err := c.doGet("/proxies")
	if err != nil {
		return nil, err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	proxiesRaw, ok := raw["proxies"]
	if !ok {
		return nil, fmt.Errorf("unexpected API response format")
	}

	var proxies map[string]json.RawMessage
	if err := json.Unmarshal(proxiesRaw, &proxies); err != nil {
		return nil, fmt.Errorf("parse proxies: %w", err)
	}

	groupTypes := map[string]bool{
		"Selector": true, "URLTest": true, "Fallback": true, "LoadBalance": true,
	}

	var groups []GroupInfo
	for name, raw := range proxies {
		var p struct {
			Type string   `json:"type"`
			Now  string   `json:"now"`
			All  []string `json:"all"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			continue
		}
		if groupTypes[p.Type] {
			groups = append(groups, GroupInfo{Name: name, Type: p.Type, Now: p.Now, All: p.All})
		}
	}

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name < groups[j].Name
	})
	return groups, nil
}

func (c *Client) SwitchNode(group, node string) error {
	body, _ := json.Marshal(map[string]string{"name": node})
	url := fmt.Sprintf("%s/proxies/%s", c.BaseURL, group)
	req, err := http.NewRequest("PUT", url, io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("API %d: %s", resp.StatusCode, string(data))
	}
	return nil
}

func (c *Client) doGet(path string) ([]byte, error) {
	url := c.BaseURL + path
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if c.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("API %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}
