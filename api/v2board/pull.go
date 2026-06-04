package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

type SyncResult struct {
	Node         *NodeInfo
	NodeChanged  bool
	Users        []UserInfo
	UsersChanged bool
	Alive        map[int]int
	AliveChanged bool
}

type UnifiedResponseError struct {
	Err error
}

func (e *UnifiedResponseError) Error() string {
	return e.Err.Error()
}

func (e *UnifiedResponseError) Unwrap() error {
	return e.Err
}

type unifiedBody struct {
	Message   string            `json:"message"`
	Config    json.RawMessage   `json:"config"`
	User      json.RawMessage   `json:"user"`
	AliveList json.RawMessage   `json:"alivelist"`
	Etags     map[string]string `json:"etags"`
}

type unifiedRequest struct {
	NodeID     int               `json:"node_id,omitempty"`
	Etags      map[string]string `json:"etags,omitempty"`
	Traffic    map[int][]int64   `json:"traffic,omitempty"`
	Alive      map[int][]string  `json:"alive,omitempty"`
	NodeStatus *NodeStatus       `json:"nodestatus,omitempty"`
}

type segmentState struct {
	NotModified bool `json:"not_modified"`
	Unchanged   bool `json:"unchanged"`
}

func (c *Client) Bootstrap(ctx context.Context) (*SyncResult, error) {
	return c.postUnified(ctx, nil, true)
}

func (c *Client) postUnified(ctx context.Context, body *unifiedRequest, allowRetry bool) (*SyncResult, error) {
	client := c.client
	if !allowRetry && c.syncClient != nil {
		client = c.syncClient
	}

	req := client.R().SetContext(ctx)
	if body != nil {
		req.SetBody(body).ForceContentType("application/json")
	}

	r, err := req.Post("")
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, fmt.Errorf("unified api failed: received nil response")
	}
	if r.StatusCode() == 304 {
		return &SyncResult{}, nil
	}
	if !r.IsSuccess() {
		return nil, fmt.Errorf("unified api failed: status %d: %s", r.StatusCode(), string(r.Body()))
	}

	var response unifiedBody
	if err := json.Unmarshal(r.Body(), &response); err != nil {
		return nil, &UnifiedResponseError{Err: fmt.Errorf("decode unified response error: %w", err)}
	}

	result := &SyncResult{}
	if segmentHasData(response.Config) {
		node, err := c.parseNodeInfo(response.Config)
		if err != nil {
			return nil, &UnifiedResponseError{Err: err}
		}
		result.Node = node
		result.NodeChanged = true
	}

	if segmentHasData(response.User) {
		var userList UserListBody
		if err := json.Unmarshal(response.User, &userList); err != nil {
			return nil, &UnifiedResponseError{Err: fmt.Errorf("decode unified user segment error: %w", err)}
		}
		result.Users = userList.Users
		result.UsersChanged = true
	}

	if segmentHasData(response.AliveList) {
		alive, err := parseAliveList(response.AliveList)
		if err != nil {
			return nil, &UnifiedResponseError{Err: fmt.Errorf("decode unified alivelist segment error: %w", err)}
		}
		result.Alive = alive
		result.AliveChanged = true
	}

	if c.etags == nil {
		c.etags = make(map[string]string)
	}
	for key, etag := range response.Etags {
		c.etags[key] = etag
	}

	return result, nil
}

func (c *Client) copyEtags() map[string]string {
	if len(c.etags) == 0 {
		return nil
	}
	etags := make(map[string]string, len(c.etags))
	for key, etag := range c.etags {
		etags[key] = etag
	}
	return etags
}

func parseAliveList(raw json.RawMessage) (map[int]int, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err == nil {
		if wrapped, ok := object["alive"]; ok {
			alive := make(map[int]int)
			if !segmentHasData(wrapped) {
				return alive, nil
			}
			if err := json.Unmarshal(wrapped, &alive); err != nil {
				return nil, err
			}
			return alive, nil
		}
	}

	alive := make(map[int]int)
	if !segmentHasData(raw) {
		return alive, nil
	}
	if err := json.Unmarshal(raw, &alive); err != nil {
		return nil, err
	}
	return alive, nil
}

func segmentHasData(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return false
	}
	var state segmentState
	if err := json.Unmarshal(raw, &state); err == nil && (state.NotModified || state.Unchanged) {
		return false
	}
	return true
}
