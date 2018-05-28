package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/container/v1beta1"
)

const (
	// operationWaitTimeoutSecond define the time wait in second before assuming the failure of a GCloud operation
	operationWaitTimeoutSecond = 600

	// operationPollIntervalSecond define the interval in second before each GCloud operation status check
	operationPollIntervalSecond = 10
)

type GCloud struct {
	Client   *http.Client
	Cluster  string
	Context  context.Context
	Project  string
	Location string
}

type GCloudClient interface {
	GetProjectDetailsFromNode(string) error
	NewGCloudContainerClient() (GCloudContainerClient, error)
}

// NewGCloudClient return a GCloud client
func NewGCloudClient() (gcloud GCloudClient, err error) {
	ctx := context.Background()
	client, err := google.DefaultClient(ctx, container.CloudPlatformScope)

	if err != nil {
		err = fmt.Errorf("Error creating GCloud client:\n%v", err)
	}

	gcloud = &GCloud{
		Client:  client,
		Context: ctx,
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
		Client:  g,
		Service: service,
	}

	return
}

// GetProjectDetailsFromNode retrieve project id, zone and cluster id from a given node spec provider id
func (g *GCloud) GetProjectDetailsFromNode(providerId string) (err error) {
	s := strings.Split(providerId, "/")

	g.Project = s[2]

	service, err := compute.New(g.Client)

	if err != nil {
		err = fmt.Errorf("Error creating GCloud compute client: %v", err)
		return
	}

	node, err := service.Instances.Get(g.Project, s[3], s[4]).Context(g.Context).Do()

	if err != nil {
		err = fmt.Errorf("Error retrieving instance details from GCloud: %v", err)
		return
	}

	// get cluster name from node metadata
	for _, metadata := range node.Metadata.Items {
		if metadata.Key == "cluster-name" {
			g.Cluster = *metadata.Value
		}
		if metadata.Key == "cluster-location" {
			g.Location = *metadata.Value
		}
		if g.Cluster != "" && g.Location != "" {
			break
		}
	}

	return
}
