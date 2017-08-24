package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/ericchiang/k8s"
	apiv1 "github.com/ericchiang/k8s/api/v1"
	"github.com/ghodss/yaml"
)

type Kubernetes struct {
	Client *k8s.Client
}

type KubernetesClient interface {
	GetNode(string) (*apiv1.Node, error)
	GetNodeList(string) (*apiv1.NodeList, error)
	GetProjectIdAndZoneFromNode(string) (string, string, error)
}

// NewKubernetesClient return a Kubernetes client
func NewKubernetesClient(host string, port string, namespace string, kubeConfigPath string) (kubernetes KubernetesClient, err error) {
	var client *k8s.Client

	if len(host) > 0 && len(port) > 0 {
		client, err = k8s.NewInClusterClient()

		if err != nil {
			err = fmt.Errorf("Error loading incluster client:\n%v", err)
			return
		}
	} else if len(kubeConfigPath) > 0 {
		client, err = loadK8sClient(kubeConfigPath)

		if err != nil {
			err = fmt.Errorf("Error loading client using kubeconfig:\n%v", err)
			return
		}
	} else {
		if namespace == "" {
			namespace = "default"
		}

		client = &k8s.Client{
			Endpoint:  "http://127.0.0.1:8001",
			Namespace: namespace,
			Client:    &http.Client{},
		}
	}

	kubernetes = &Kubernetes{
		Client: client,
	}

	return
}

// GetProjectIdAndZoneFromNode returns project id and zone from given node name
// by getting informations from node spec provider id
func (k *Kubernetes) GetProjectIdAndZoneFromNode(name string) (projectId string, zone string, err error) {
	node, err := k.GetNode(name)

	if err != nil {
		return
	}

	s := strings.Split(*node.Spec.ProviderID, "/")
	projectId = s[2]
	zone = s[3]

	return
}

// GetNode return the node object from given name
func (k *Kubernetes) GetNode(name string) (node *apiv1.Node, err error) {
	node, err = k.Client.CoreV1().GetNode(context.Background(), name)
	return
}

// GetNodeList return a list of nodes from a given node pool name
func (k *Kubernetes) GetNodeList(name string) (nodes *apiv1.NodeList, err error) {
	labels := new(k8s.LabelSelector)
	labels.Eq("cloud.google.com/gke-nodepool", name)
	nodes, err = k.Client.CoreV1().ListNodes(context.Background(), labels.Selector())
	return
}

// loadK8sClient parses a kubeconfig from a file and returns a Kubernetes
// client. It does not support extensions or client auth providers.
func loadK8sClient(kubeconfigPath string) (*k8s.Client, error) {
	data, err := ioutil.ReadFile(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("Read kubeconfig error:\n%v", err)
	}

	// Unmarshal YAML into a Kubernetes config object.
	var config k8s.Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("Unmarshal kubeconfig error:\n%v", err)
	}

	// fmt.Printf("%#v", config)
	return k8s.NewClient(&config)
}
