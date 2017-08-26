# estafette-gke-node-pool-shifter

This controller shifts nodes from one node pool to another, in order to favour preemptibles over a 'safety net' node pool of regular vms.

[![License](https://img.shields.io/github/license/estafette/estafette-gke-node-pool-shifter.svg)](https://github.com/estafette/estafette-gke-node-pool-shifter/blob/master/LICENSE)


## Usage

You can either use environment variables or flags to configure the following settings:

| Environment variable   | Flag                     | Default  | Description
| ---------------------- | ------------------------ | -------- | -----------------------------------------------------------------
| INTERVAL               | --interval (-i)          | 600      | Time in second to wait between each shift check
| KUBECONFIG             | --kubeconfig             |          | Provide the path to the kube config path, usually located in ~/.kube/config. For out of cluster execution
| METRICS_LISTEN_ADDRESS | --metrics-listen-address | :9001    | The address to listen on for Prometheus metrics requests
| METRICS_PATH           | --metrics-path           | /metrics | The path to listen for Prometheus metrics requests
| NODE_POOL_FROM         | --node-pool-from         |          | Name of the node pool to shift from
| NODE_POOL_TO           | --node-pool-to           |          | Name of the node pool to shift to


### In cluster

As a Kubernetes administrator, you first need to deploy the rbac.yaml file which set role and permissions.
Then deploy the application to Kubernetes cluster using the manifest below.


```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: estafette
---
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: estafette-gke-node-pool-shifter
  namespace: estafette
  labels:
    app: estafette-gke-node-pool-shifter
spec:
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app: estafette-gke-node-pool-shifter
  template:
    metadata:
      labels:
        app: estafette-gke-node-pool-shifter
    spec:
      serviceAccount: estafette-gke-node-pool-shifter
      terminationGracePeriodSeconds: 600
      containers:
      - name: estafette-gke-node-pool-shifter
        image: estafette/estafette-gke-node-pool-shifter:latest
        ports:
        - name: prom-metrics
          containerPort: 9001
        env:
        - name: NODE_POOL_FROM
          value: default-pool
        - name: NODE_POOL_TO
          value: preemptible-pool
        resources:
          requests:
            cpu: 10m
            memory: 16Mi
          limits:
            cpu: 50m
            memory: 128Mi
        livenessProbe:
          httpGet:
            path: /metrics
            port: prom-metrics
          initialDelaySeconds: 30
          timeoutSeconds: 1
```

### Local development

For development purpose, you can create a new cluster with 2 autoscaled node pools, 1 preemptible and 1 regular VM.

#### Create the cluster with appropriate node pools

```
export CLUSTER_NAME=node-shifter
export CLUSTER_VERSION=1.7.3
export PROJECT=my-project
export ZONE=europe-west1-c

# Create cluster with regular VMs
gcloud beta container clusters create $CLUSTER_NAME \
  --project=$PROJECT \
  --zone=$ZONE \
  --cluster-version=$CLUSTER_VERSION \
  --num-nodes=1 \
  --enable-autoscaling \
  --min-nodes=0 \
  --max-nodes=3

# Add preemptible VMs node pool
gcloud beta container node-pools create preemptible-pool \
  --project=$PROJECT \
  --zone=$ZONE \
  --cluster=$CLUSTER_NAME \
  --num-nodes=1  \
  --enable-autoscaling \
  --min-nodes=1 \
  --max-nodes=3 \
  --preemptible
```

#### Deploy an application

```
kubectl run nginx --image=nginx:alpine --replicas=5 --limits='cpu=200m,memory=512Mi'
```

#### Start the node pool shifter

```
# proxy master
kubectl proxy

# in another shell
go build && ./estafette-gke-node-pool-shifter --node-pool-from=default-pool --node-pool-to=preemptible-pool
```

Note: `KUBECONFIG=~/.kube/config` as environment variable can also be used if you don't want to use the `kubectl proxy`
command.

If necessary, you can resize the node pool resize:
```
gcloud container clusters resize $CLUSTER_NAME
  --project=$PROJECT \
  --zone=$ZONE \
  --size=1 \
  --node-pool=default-pool
```
