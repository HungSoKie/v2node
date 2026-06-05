package panel

import (
	"context"
	"time"
)

type NodeStatus struct {
	Online    bool  `json:"online"`
	Timestamp int64 `json:"timestamp"`
}

func (c *Client) Sync(ctx context.Context, userTraffic []UserTraffic, alive map[int][]string) (*SyncResult, error) {
	traffic := make(map[int][]int64, len(userTraffic))
	for i := range userTraffic {
		traffic[userTraffic[i].UID] = []int64{userTraffic[i].Upload, userTraffic[i].Download}
	}

	body := &unifiedRequest{
		NodeID: c.NodeId,
		Etag:   c.copyEtags(),
		NodeStatus: &NodeStatus{
			Online:    true,
			Timestamp: time.Now().Unix(),
		},
	}
	if len(traffic) > 0 {
		body.Traffic = traffic
	}
	if len(alive) > 0 {
		body.Alive = alive
	}

	return c.postUnified(ctx, body, false)
}
