package panel

import (
	"context"
	"fmt"
	"time"
)

const pushPath = "/api/v2/server/push"

type NodeStatus struct {
	Online    bool  `json:"online"`
	Timestamp int64 `json:"timestamp"`
}

type PushBody struct {
	Traffic    map[int][]int64  `json:"traffic"`
	Alive      map[int][]string `json:"alive"`
	NodeStatus NodeStatus       `json:"nodestatus"`
}

func (c *Client) Push(ctx context.Context, userTraffic []UserTraffic, alive map[int][]string) error {
	traffic := make(map[int][]int64, len(userTraffic))
	for i := range userTraffic {
		traffic[userTraffic[i].UID] = []int64{userTraffic[i].Upload, userTraffic[i].Download}
	}
	if alive == nil {
		alive = make(map[int][]string)
	}

	r, err := c.client.R().
		SetContext(ctx).
		SetBody(PushBody{
			Traffic: traffic,
			Alive:   alive,
			NodeStatus: NodeStatus{
				Online:    true,
				Timestamp: time.Now().Unix(),
			},
		}).
		ForceContentType("application/json").
		Post(pushPath)
	if err != nil {
		return err
	}
	if r == nil {
		return fmt.Errorf("push failed: received nil response")
	}
	if !r.IsSuccess() {
		return fmt.Errorf("push failed: status %d: %s", r.StatusCode(), string(r.Body()))
	}
	return nil
}
