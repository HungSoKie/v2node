package node

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"
	panel "github.com/wyx2685/v2node/api/v2board"
	"github.com/wyx2685/v2node/conf"
	"github.com/wyx2685/v2node/core"
)

type Node struct {
	controllers []*Controller
	NodeInfos   []*panel.NodeInfo
}

func New(nodes []conf.NodeConfig) (*Node, error) {
	n := &Node{
		controllers: make([]*Controller, len(nodes)),
		NodeInfos:   make([]*panel.NodeInfo, len(nodes)),
	}
	for i, node := range nodes {
		p, err := panel.New(&node)
		if err != nil {
			return nil, err
		}
		pull, err := p.Pull(context.Background())
		if err != nil {
			return nil, err
		}
		if pull.Node == nil {
			return nil, fmt.Errorf("pull node info error: missing config segment")
		}
		n.controllers[i] = NewController(p, &node, pull)
		n.NodeInfos[i] = pull.Node
	}
	return n, nil
}

func (n *Node) Start(nodes []conf.NodeConfig, core *core.V2Core) error {
	for i, node := range nodes {
		err := n.controllers[i].Start(core)
		if err != nil {
			return fmt.Errorf("start node controller [%s-%d] error: %s",
				node.APIHost,
				node.NodeID,
				err)
		}
	}
	return nil
}

func (n *Node) Close() error {
	var err error
	for _, c := range n.controllers {
		if err = c.Close(); err != nil {
			log.Errorf("close controller failed: %v", err)
			return err
		}
	}
	n.controllers = nil
	return nil
}
