package panel

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/go-resty/resty/v2"
	"github.com/wyx2685/v2node/conf"
)

// Panel is the interface for different panel's api.

type Client struct {
	client     *resty.Client
	syncClient *resty.Client
	APIHost    string
	Token      string
	NodeId     int
	etags      map[string]string
	UserList   *UserListBody
	AliveMap   *AliveMap
}

func New(c *conf.NodeConfig) (*Client, error) {
	client := resty.New()
	retryCount := conf.DefaultNodeRetryCount
	if c.RetryCount != nil {
		retryCount = *c.RetryCount
	}
	client.SetRetryCount(retryCount)
	client.SetHeader("User-Agent", fmt.Sprintf("v2node unified go-resty/%s (https://github.com/go-resty/resty)", resty.Version))
	if c.Timeout > 0 {
		client.SetTimeout(time.Duration(c.Timeout) * time.Second)
	} else {
		client.SetTimeout(time.Duration(conf.DefaultNodeTimeout) * time.Second)
	}
	client.OnError(func(req *resty.Request, err error) {
		var v *resty.ResponseError
		if errors.As(err, &v) {
			// v.Response contains the last response from the server
			// v.Err contains the original error
			logrus.Error(v.Err)
		}
	})
	client.SetBaseURL(c.APIHost)
	// set params
	client.SetQueryParams(map[string]string{
		"node_type": "v2node",
		"node_id":   strconv.Itoa(c.NodeID),
		"token":     c.Key,
	})
	syncClient := client.Clone()
	syncClient.SetRetryCount(0)
	return &Client{
		client:     client,
		syncClient: syncClient,
		Token:      c.Key,
		APIHost:    c.APIHost,
		NodeId:     c.NodeID,
		etags:      make(map[string]string),
		UserList:   &UserListBody{},
		AliveMap:   &AliveMap{},
	}, nil
}
