package main

import (
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"google.golang.org/api/container/v1beta1"
)

type GCloudContainer struct {
	Client  *GCloud
	Service *container.Service
}

type GCloudContainerClient interface {
	SetNodePoolSize(string, int64) error
	waitForOperation(*container.Operation) error
}

// SetNodePoolSize set the size of a given node pool
func (gc *GCloudContainer) SetNodePoolSize(name string, size int64) (err error) {

	apiName := fmt.Sprintf("projects/%v/locations/%v/clusters/%v/nodePools/%v", gc.Client.Project, gc.Client.Location, gc.Client.Cluster, name)

	nodePoolSizeRequest := &container.SetNodePoolSizeRequest{
		NodeCount: size,
	}

	operation, err := gc.Service.Projects.Locations.Clusters.NodePools.SetSize(apiName, nodePoolSizeRequest).Context(gc.Client.Context).Do()

	if err != nil {
		return
	}

	err = gc.waitForOperation(operation)

	return
}

// waitForOperation wait for a GCloud operation to finish
func (gc *GCloudContainer) waitForOperation(operation *container.Operation) (err error) {
	start := time.Now()
	timeout := operationWaitTimeoutSecond * time.Second

	for {
		apiName := fmt.Sprintf("projects/%v/locations/%v/operations/%v", gc.Client.Project, gc.Client.Location, operation.Name)

		log.Debug().Msgf("Waiting for operation %v", apiName)

		if op, err := gc.Service.Projects.Locations.Operations.Get(apiName).Do(); err == nil {
			log.Debug().Msgf("Operation %v status: %s", apiName, op.Status)

			if op.Status == "DONE" {
				return nil
			}
		} else {
			log.Error().Err(err).Msgf("Error while getting operation %v on %s: %v", apiName, operation.TargetLink, err)
		}

		if time.Since(start) > timeout {
			err = fmt.Errorf("Timeout while waiting for operation %v on %s to complete", apiName, operation.TargetLink)
			return
		}

		sleepTime := ApplyJitter(operationPollIntervalSecond)
		log.Info().Msgf("Sleeping for %v seconds...", sleepTime)
		time.Sleep(time.Duration(sleepTime) * time.Second)
	}

	return
}
