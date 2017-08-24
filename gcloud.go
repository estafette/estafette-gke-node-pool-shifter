package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/container/v1"
)

const (
	// operationWaitTimeoutSecond define the time wait in second before assuming the failure of a GCloud operation
	operationWaitTimeoutSecond = 600

	// operationPollIntervalSecond define the interval in second before each GCloud operation status check
	operationPollIntervalSecond = 10
)

type GCloud struct {
	Client    *http.Client
	ClusterID string
	Service   *container.Service
	ProjectID string
	Zone      string
}

type GCloudClient interface {
	GetNodePool(string) (*container.NodePool, error)
	SetNodePoolSize(string, int64) error
	waitForOperation(*container.Operation) error
}

// NewGCloudClient return a GCloud client
func NewGCloudClient(projectId, zone, clusterId string) (gcloud GCloudClient, err error) {
	client, err := google.DefaultClient(context.Background(), container.CloudPlatformScope)

	if err != nil {
		err = fmt.Errorf("Error creating compute client:\n%v", err)
		return
	}

	service, err := container.New(client)

	if err != nil {
		err = fmt.Errorf("Error creating container service:\n%v", err)
		return
	}

	gcloud = &GCloud{
		Client:    client,
		ClusterID: clusterId,
		Service:   service,
		ProjectID: projectId,
		Zone:      zone,
	}

	return
}

// waitForOperation wait for a GCloud operation to finish
func (g *GCloud) waitForOperation(operation *container.Operation) error {
	start := time.Now()
	timeout := operationWaitTimeoutSecond * time.Second

	for {
		log.Debug().Msgf("Waiting for operation %s %s %s", g.ProjectID, g.Zone, operation.Name)

		if op, err := g.Service.Projects.Zones.Operations.Get(g.ProjectID, g.Zone, operation.Name).Do(); err == nil {
			log.Debug().Msgf("Operation %s %s %s status: %s", g.ProjectID, g.Zone, operation.Name, op.Status)

			if op.Status == "DONE" {
				return nil
			}
		} else {
			log.Error().Err(err).Msgf("Error while getting operation %s on %s: %v", operation.Name, operation.TargetLink, err)
		}

		if time.Since(start) > timeout {
			return fmt.Errorf("Timeout while waiting for operation %s on %s to complete.", operation.Name, operation.TargetLink)
		}

		sleepTime := ApplyJitter(operationPollIntervalSecond)
		log.Info().Msgf("Sleeping for %v seconds...", sleepTime)
		time.Sleep(time.Duration(sleepTime) * time.Second)
	}

	return
}

// GetNodePool retreive a given node pool
func (g *GCloud) GetNodePool(name string) (nodePool *container.NodePool, err error) {
	nodePool, err = g.Service.Projects.Zones.Clusters.NodePools.Get(g.ProjectID, g.Zone, g.ClusterID, name).Context(context.Background()).Do()
	return
}

// SetNodePoolSize set the size of a given node pool
func (g *GCloud) SetNodePoolSize(name string, size int64) (err error) {
	nodePoolSizeRequest := &container.SetNodePoolSizeRequest{
		NodeCount: size,
	}

	operation, err := g.Service.Projects.Zones.Clusters.NodePools.SetSize(g.ProjectID, g.Zone, g.ClusterID, name, nodePoolSizeRequest).Context(context.Background()).Do()

	if err != nil {
		return
	}

	err = g.waitForOperation(operation)

	return
}
