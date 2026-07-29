package panel

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/wyx2685/v2node/common/system"
)

type NodeStatus struct {
	Online    bool    `json:"online"`
	Timestamp int64   `json:"timestamp"`
	CPU       float64 `json:"cpu"`
	MemUsed   uint64  `json:"mem_used"`
	MemTotal  uint64  `json:"mem_total"`
	Uptime    uint64  `json:"uptime"`
}

func (c *Client) Sync(ctx context.Context, userTraffic []UserTraffic, alive map[int][]string) (*SyncResult, error) {
	traffic := make(map[int][]int64, len(userTraffic))
	for i := range userTraffic {
		traffic[userTraffic[i].UID] = []int64{userTraffic[i].Upload, userTraffic[i].Download}
	}

	stats, err := system.Collect()
	if err != nil {
		logrus.WithError(err).Warn("failed to collect system stats")
	}

	body := &unifiedRequest{
		NodeID: c.NodeId,
		Etag:   c.copyEtags(),
		NodeStatus: &NodeStatus{
			Online:    true,
			Timestamp: time.Now().Unix(),
			CPU:       stats.CPUPercent,
			MemUsed:   stats.MemUsed,
			MemTotal:  stats.MemTotal,
			Uptime:    stats.Uptime,
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
