package node

import (
	"time"

	log "github.com/sirupsen/logrus"
	panel "github.com/wyx2685/v2node/api/v2board"
	"github.com/wyx2685/v2node/common/task"
	vCore "github.com/wyx2685/v2node/core"
)

func (c *Controller) startTasks(node *panel.NodeInfo) {
	c.syncPeriodic = &task.Task{
		Name:     "unifiedSyncTask",
		Interval: syncInterval(node),
		Execute:  c.unifiedSyncTask,
		ReloadCh: c.server.ReloadCh,
	}
	log.WithField("tag", c.tag).Info("Start unified sync task")
	_ = c.syncPeriodic.Start(false)
	if node.Security == panel.Tls {
		switch c.info.Common.CertInfo.CertMode {
		case "none", "", "file", "self":
		default:
			c.renewCertPeriodic = &task.Task{
				Name:     "renewCertTask",
				Interval: time.Hour * 24,
				Execute:  c.renewCertTask,
				ReloadCh: c.server.ReloadCh,
			}
			log.WithField("tag", c.tag).Info("Start renew cert")
			// delay to start renewCert
			_ = c.renewCertPeriodic.Start(true)
		}
	}
}

func syncInterval(node *panel.NodeInfo) time.Duration {
	interval := node.PushInterval
	if interval <= 0 || node.PullInterval > 0 && node.PullInterval < interval {
		interval = node.PullInterval
	}
	return interval
}

func (c *Controller) handlePanelData(syncResult *panel.SyncResult) {
	if syncResult == nil {
		return
	}
	if syncResult.NodeChanged {
		log.WithFields(log.Fields{
			"tag": c.tag,
		}).Error("Got new node info, reload")
		if c.server.ReloadCh != nil {
			select {
			case c.server.ReloadCh <- struct{}{}:
			default:
			}
		} else {
			log.Panic("Reload failed")
		}
		return
	}
	log.WithField("tag", c.tag).Debug("Node info no change")

	// update alive list
	if syncResult.AliveChanged {
		c.limiter.AliveList = syncResult.Alive
	}
	// node no changed, check users
	if !syncResult.UsersChanged {
		log.WithField("tag", c.tag).Debug("User list no change")
		return
	}
	newU := syncResult.Users
	deleted, added, modified := compareUserList(c.userList, newU)
	if len(deleted) > 0 {
		// have deleted users
		if err := c.server.DelUsers(deleted, c.tag, c.info); err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Delete users failed")
			return
		}
	}
	if len(added) > 0 {
		// have added users
		_, err := c.server.AddUsers(&vCore.AddUsersParams{
			Tag:      c.tag,
			NodeInfo: c.info,
			Users:    added,
		})
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Add users failed")
			return
		}
	}
	if len(added) > 0 || len(deleted) > 0 || len(modified) > 0 {
		// update Limiter
		c.limiter.UpdateUser(c.tag, added, deleted, modified)
	}
	c.userList = newU
	log.WithField("tag", c.tag).Infof("%d user deleted, %d user added, %d user modified", len(deleted), len(added), len(modified))
}
