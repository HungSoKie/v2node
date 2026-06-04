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
	server            *core.V2Core
	apiClient         *panel.Client
	tag               string
	limiter           *limiter.Limiter
	userList          []panel.UserInfo
	aliveMap          map[int]int
	conf              *conf.NodeConfig
	info              *panel.NodeInfo
	initialSync       *panel.SyncResult
	syncPeriodic      *task.Task
	renewCertPeriodic *task.Task
}

// NewController return a Node controller with default parameters.
func NewController(api *panel.Client, conf *conf.NodeConfig, initialSync *panel.SyncResult) *Controller {
	controller := &Controller{
		apiClient:   api,
		info:        initialSync.Node,
		conf:        conf,
		initialSync: initialSync,
	}
	return controller
}

// Start implement the Start() function of the service interface
func (c *Controller) Start(x *core.V2Core) error {
	// Init Core
	c.server = x
	var err error
	syncResult := c.initialSync
	c.initialSync = nil
	if syncResult == nil {
		syncResult, err = c.apiClient.Bootstrap(context.Background())
		if err != nil {
			return fmt.Errorf("bootstrap node data error: %s", err)
		}
	}
	if syncResult.Node != nil {
		c.info = syncResult.Node
	}
	node := c.info
	if node == nil {
		return fmt.Errorf("bootstrap node info error: missing config segment")
	}
	if !syncResult.UsersChanged {
		return fmt.Errorf("bootstrap user list error: missing user segment")
	}
	c.userList = syncResult.Users
	if len(c.userList) == 0 {
		return errors.New("add users error: not have any user")
	}
	if syncResult.AliveChanged {
		c.aliveMap = syncResult.Alive
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
	if c.syncPeriodic != nil {
		c.syncPeriodic.Close()
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
