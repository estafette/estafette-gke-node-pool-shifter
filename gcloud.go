package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/container/v1"
)

const (
	// operationWaitTimeoutSecond define the time wait in second before assuming the failure of a GCloud operation
	operationWaitTimeoutSecond = 600

	// operationPollIntervalSecond define the interval in second before each GCloud operation status check
	operationPollIntervalSecond = 10
)

type GCloud struct {
	Client  *http.Client
	Project string
	Cluster string
	Zone    string
}

type GCloudContainer struct {
	Service *container.Service
	Project string
	Cluster string
	Zone    string
}

type GCloudClient interface {
	GetProjectDetailsFromNode(string) error
	NewGCloudContainerClient() (GCloudContainerClient, error)
}

type GCloudContainerClient interface {
	GetNodePool(string) (*container.NodePool, error)
	SetNodePoolSize(string, int64) error
	waitForOperation(*container.Operation) error
}

// NewGCloudClient return a GCloud client
func NewGCloudClient() (gcloud GCloudClient, err error) {
	client, err := google.DefaultClient(context.Background(), compute.CloudPlatformScope)

	if err != nil {
		err = fmt.Errorf("Error creating GCloud client:\n%v", err)
	}

	gcloud = &GCloud{
		Client: client,
	}

	return
}

// NewGCloudContainerClient return a GCloud container client
func (g *GCloud) NewGCloudContainerClient() (gcloud GCloudContainerClient, err error) {
	service, err := container.New(g.Client)

	if err != nil {
		err = fmt.Errorf("Error creating GCloud container client:\n%v", err)
		return
	}

	gcloud = &GCloudContainer{
		Cluster: g.Cluster,
		Project: g.Project,
		Service: service,
		Zone:    g.Zone,
	}

	return
}

// GetProjectDetailsFromNode retrieve project id, zone and cluster id from a given node spec provider id
func (g *GCloud) GetProjectDetailsFromNode(providerId string) (err error) {
	s := strings.Split(providerId, "/")

	g.Project = s[2]
	g.Zone = s[3]

	service, err := compute.New(g.Client)

	if err != nil {
		err = fmt.Errorf("Error creating GCloud compute client: %v", err)
		return
	}

	node, err := service.Instances.Get(g.Project, g.Zone, s[4]).Context(context.Background()).Do()

	if err != nil {
		err = fmt.Errorf("Error retrieving instance details from GCloud: %v", err)
		return
	}

	// get cluster name from node metadata
	for _, metadata := range node.Metadata.Items {
		if metadata.Key == "cluster-name" {
			g.Cluster = *metadata.Value
			return
		}
	}

	return
}

// GetNodePool retrieve a given node pool
func (g *GCloudContainer) GetNodePool(name string) (nodePool *container.NodePool, err error) {
	nodePool, err = g.Service.Projects.Zones.Clusters.NodePools.Get(g.Project, g.Zone, g.Cluster, name).Context(context.Background()).Do()
	return
}

// SetNodePoolSize set the size of a given node pool
func (g *GCloudContainer) SetNodePoolSize(name string, size int64) (err error) {
	nodePoolSizeRequest := &container.SetNodePoolSizeRequest{
		NodeCount: size,
	}

	operation, err := g.Service.Projects.Zones.Clusters.NodePools.SetSize(g.Project, g.Zone, g.Cluster, name, nodePoolSizeRequest).Context(context.Background()).Do()

	if err != nil {
		return
	}

	err = g.waitForOperation(operation)

	return
}

// waitForOperation wait for a GCloud operation to finish
func (g *GCloudContainer) waitForOperation(operation *container.Operation) (err error) {
	start := time.Now()
	timeout := operationWaitTimeoutSecond * time.Second

	for {
		log.Debug().Msgf("Waiting for operation %s %s %s", g.Project, g.Zone, operation.Name)

		if op, err := g.Service.Projects.Zones.Operations.Get(g.Project, g.Zone, operation.Name).Do(); err == nil {
			log.Debug().Msgf("Operation %s %s %s status: %s", g.Project, g.Zone, operation.Name, op.Status)

			if op.Status == "DONE" {
				return nil
			}
		} else {
			log.Error().Err(err).Msgf("Error while getting operation %s on %s: %v", operation.Name, operation.TargetLink, err)
		}

		if time.Since(start) > timeout {
			err = fmt.Errorf("Timeout while waiting for operation %s on %s to complete.", operation.Name, operation.TargetLink)
			return
		}

		sleepTime := ApplyJitter(operationPollIntervalSecond)
		log.Info().Msgf("Sleeping for %v seconds...", sleepTime)
		time.Sleep(time.Duration(sleepTime) * time.Second)
	}

	return
}
