# estafette-gke-node-pool-shifter

This controller shifts nodes from one node pool to another, in order to favour preemptibles over a 'safety net' node
pool of regular vms.

[![License](https://img.shields.io/github/license/estafette/estafette-gke-node-pool-shifter.svg)](https://github.com/estafette/estafette-gke-node-pool-shifter/blob/master/LICENSE)


## Usage

You can either use environment variables or flags to configure the following settings:

| Environment variable    | Flag                      | Default  | Description
| ----------------------- | ------------------------- | -------- | ----------------------------------------------------
| INTERVAL                | --interval (-i)           | 300      | Time in second to wait between each shift check
| KUBECONFIG              | --kubeconfig              |          | Provide the path to the kube config path, usually located in ~/.kube/config. For out of cluster execution
| METRICS_LISTEN_ADDRESS  | --metrics-listen-address  | :9001    | The address to listen on for Prometheus metrics requests
| METRICS_PATH            | --metrics-path            | /metrics | The path to listen for Prometheus metrics requests
| NODE_POOL_FROM          | --node-pool-from          |          | Name of the node pool to shift from
| NODE_POOL_FROM_MIN_NODE | --node-pool-from-min-node | 0        | Minimum amount of node to keep on the from node pool
| NODE_POOL_TO            | --node-pool-to            |          | Name of the node pool to shift to

*Before deploying*, you first need to create a service account via the GCloud dashboard with role set to _Compute
Instance Admin_ and _Container Engine Admin_. This key is going to be used to authenticate from the application to
the GCloud API. See [documentation](https://developers.google.com/identity/protocols/application-default-credentials).


### Deploy with Helm

```
brew install kubernetes-helm
helm init --history-max 25 --upgrade
helm package chart/estafette-gke-node-pool-shifter --version 1.0.11
helm upgrade estafette-gke-node-pool-shifter estafette-gke-node-pool-shifter-1.0.11.tgz --namespace estafette --install --set rbac.create=true --set googleServiceAccount=$(./google_service_account.json | base64)
```

### Deploy without Helm

```
export NAMESPACE=estafette
export APP_NAME=estafette-gke-node-pool-shifter
export TEAM_NAME=tooling
export VERSION=1.0.11
export GO_PIPELINE_LABEL=1.0.11
export GOOGLE_SERVICE_ACCOUNT=$(cat google-service-account.json | base64)
export INTERVAL=300
export NODE_POOL_FROM=default-pool
export NODE_POOL_TO=preemptible-pool
export NODE_POOL_FROM_MIN_NODE=0
export CPU_REQUEST=10m
export MEMORY_REQUEST=16Mi
export CPU_LIMIT=50m
export MEMORY_LIMIT=128Mi

# Setup RBAC
curl https://raw.githubusercontent.com/estafette/estafette-gke-node-pool-shifter/master/rbac.yaml | envsubst | kubectl apply -n ${NAMESPACE} -f -

# Run application
curl https://raw.githubusercontent.com/estafette/estafette-gke-node-pool-shifter/master/kubernetes.yaml | envsubst | kubectl apply -n ${NAMESPACE} -f -
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

If necessary, you can resize the node pool size:
```
gcloud container clusters resize $CLUSTER_NAME
  --project=$PROJECT \
  --zone=$ZONE \
  --size=1 \
  --node-pool=default-pool
```
