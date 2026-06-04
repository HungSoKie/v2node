package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

const pullPath = "/api/v2/server/pull"

type PullResult struct {
	Node         *NodeInfo
	NodeChanged  bool
	Users        []UserInfo
	UsersChanged bool
	Alive        map[int]int
	AliveChanged bool
}

type pullBody struct {
	Config    json.RawMessage   `json:"config"`
	User      json.RawMessage   `json:"user"`
	AliveList json.RawMessage   `json:"alivelist"`
	Etags     map[string]string `json:"etags"`
}

type pullRequest struct {
	Etags map[string]string `json:"etags"`
}

type segmentState struct {
	NotModified bool `json:"not_modified"`
	Unchanged   bool `json:"unchanged"`
}

func (c *Client) Pull(ctx context.Context) (*PullResult, error) {
	etags := c.etags
	if etags == nil {
		etags = make(map[string]string)
	}
	r, err := c.client.R().
		SetContext(ctx).
		SetBody(pullRequest{Etags: etags}).
		ForceContentType("application/json").
		Post(pullPath)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, fmt.Errorf("pull failed: received nil response")
	}
	if r.StatusCode() == 304 {
		return &PullResult{}, nil
	}
	if !r.IsSuccess() {
		return nil, fmt.Errorf("pull failed: status %d: %s", r.StatusCode(), string(r.Body()))
	}

	var body pullBody
	if err := json.Unmarshal(r.Body(), &body); err != nil {
		return nil, fmt.Errorf("decode pull response error: %w", err)
	}
	for key, etag := range body.Etags {
		c.etags[key] = etag
	}

	result := &PullResult{}
	if segmentHasData(body.Config) {
		node, err := c.parseNodeInfo(body.Config)
		if err != nil {
			return nil, err
		}
		result.Node = node
		result.NodeChanged = true
	}

	if segmentHasData(body.User) {
		var userList UserListBody
		if err := json.Unmarshal(body.User, &userList); err != nil {
			return nil, fmt.Errorf("decode pull user segment error: %w", err)
		}
		result.Users = userList.Users
		result.UsersChanged = true
	}

	if segmentHasData(body.AliveList) {
		var alive AliveMap
		if err := json.Unmarshal(body.AliveList, &alive); err != nil {
			return nil, fmt.Errorf("decode pull alivelist segment error: %w", err)
		}
		if alive.Alive == nil {
			alive.Alive = make(map[int]int)
		}
		result.Alive = alive.Alive
		result.AliveChanged = true
	}

	return result, nil
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
