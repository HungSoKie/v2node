package node

import (
	"context"
	"errors"
	"fmt"

	log "github.com/sirupsen/logrus"
	panel "github.com/wyx2685/v2node/api/v2board"
	"github.com/wyx2685/v2node/common/task"
	"github.com/wyx2685/v2node/conf"
	"github.com/wyx2685/v2node/core"
	"github.com/wyx2685/v2node/limiter"
)

type Controller struct {
	server                  *core.V2Core
	apiClient               *panel.Client
	tag                     string
	limiter                 *limiter.Limiter
	userList                []panel.UserInfo
	aliveMap                map[int]int
	conf                    *conf.NodeConfig
	info                    *panel.NodeInfo
	initialPull             *panel.PullResult
	nodeInfoMonitorPeriodic *task.Task
	userReportPeriodic      *task.Task
	renewCertPeriodic       *task.Task
}

// NewController return a Node controller with default parameters.
func NewController(api *panel.Client, conf *conf.NodeConfig, initialPull *panel.PullResult) *Controller {
	controller := &Controller{
		apiClient:   api,
		info:        initialPull.Node,
		conf:        conf,
		initialPull: initialPull,
	}
	return controller
}

// Start implement the Start() function of the service interface
func (c *Controller) Start(x *core.V2Core) error {
	// Init Core
	c.server = x
	var err error
	pull := c.initialPull
	c.initialPull = nil
	if pull == nil {
		pull, err = c.apiClient.Pull(context.Background())
		if err != nil {
			return fmt.Errorf("pull node data error: %s", err)
		}
	}
	if pull.Node != nil {
		c.info = pull.Node
	}
	node := c.info
	if node == nil {
		return fmt.Errorf("pull node info error: missing config segment")
	}
	if !pull.UsersChanged {
		return fmt.Errorf("pull user list error: missing user segment")
	}
	c.userList = pull.Users
	if len(c.userList) == 0 {
		return errors.New("add users error: not have any user")
	}
	if pull.AliveChanged {
		c.aliveMap = pull.Alive
	}
	if c.aliveMap == nil {
		c.aliveMap = make(map[int]int)
	}
	c.tag = node.Tag

	// add limiter
	l := limiter.AddLimiter(c.info.Type, c.tag, c.userList, c.aliveMap)
	c.limiter = l
	if node.Security == panel.Tls {
		err = c.requestCert()
		if err != nil {
			return fmt.Errorf("request cert error: %s", err)
		}
	}
	// Add new tag
	err = c.server.AddNode(c.tag, node)
	if err != nil {
		return fmt.Errorf("add new node error: %s", err)
	}
	added, err := c.server.AddUsers(&core.AddUsersParams{
		Tag:      c.tag,
		Users:    c.userList,
		NodeInfo: node,
	})
	if err != nil {
		return fmt.Errorf("add users error: %s", err)
	}
	log.WithField("tag", c.tag).Infof("Added %d new users", added)
	c.info = node
	c.startTasks(node)
	return nil
}

// Close implement the Close() function of the service interface
func (c *Controller) Close() error {
	limiter.DeleteLimiter(c.tag)
	if c.nodeInfoMonitorPeriodic != nil {
		c.nodeInfoMonitorPeriodic.Close()
	}
	if c.userReportPeriodic != nil {
		c.userReportPeriodic.Close()
	}
	if c.renewCertPeriodic != nil {
		c.renewCertPeriodic.Close()
	}
	err := c.server.DelNode(c.tag)
	if err != nil {
		return fmt.Errorf("del node error: %s", err)
	}
	return nil
}
