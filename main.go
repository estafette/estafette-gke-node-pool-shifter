package main

import (
	stdlog "log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	apiv1 "github.com/ericchiang/k8s/api/v1"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// flags
	interval = kingpin.Flag("interval", "Time in second to wait between each node pool check.").
			Envar("INTERVAL").
			Default("300").
			Short('i').
			Int()
	kubeConfigPath = kingpin.Flag("kubeconfig", "Provide the path to the kube config path, usually located in ~/.kube/config. For out of cluster execution").
			Envar("KUBECONFIG").
			String()
	nodePoolFrom = kingpin.Flag("node-pool-from", "The name of the node pool to shift from.").
			Required().
			Envar("NODE_POOL_FROM").
			String()
	nodePoolTo = kingpin.Flag("node-pool-to", "The name of the node pool to shift to.").
			Required().
			Envar("NODE_POOL_TO").
			String()
	nodePoolFromMinNode = kingpin.Flag("node-pool-from-min-node", "The minimum number of node to keep for the node pool to shift.").
				Envar("NODE_POOL_FROM_MIN_NODE").
				Default("0").
				Int()
	prometheusAddress = kingpin.Flag("metrics-listen-address", "The address to listen on for Prometheus metrics requests.").
				Envar("METRICS_LISTEN_ADDRESS").
				Default(":9001").
				String()
	prometheusMetricsPath = kingpin.Flag("metrics-path", "The path to listen for Prometheus metrics requests.").
				Envar("METRICS_PATH").
				Default("/metrics").
				String()

	// define prometheus counter
	nodeTotals = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "estafette_gke_node_pool_shifter_node_totals",
			Help: "Number of processed nodes.",
		},
		[]string{"status"},
	)

	// application version
	version   string
	branch    string
	revision  string
	buildDate string
	goVersion = runtime.Version()
)

func init() {
	// Metrics have to be registered to be exposed:
	prometheus.MustRegister(nodeTotals)
}

func main() {
	kingpin.Parse()

	// log as severity for stackdriver logging to recognize the level
	zerolog.LevelFieldName = "severity"

	// set some default fields added to all logs
	log.Logger = zerolog.New(os.Stdout).With().
		Timestamp().
		Str("app", "estafette-gke-node-pool-shifter").
		Str("version", version).
		Logger()

	// use zerolog for any logs sent via standard log library
	stdlog.SetFlags(0)
	stdlog.SetOutput(log.Logger)

	// log startup message
	log.Info().
		Str("branch", branch).
		Str("revision", revision).
		Str("buildDate", buildDate).
		Str("goVersion", goVersion).
		Msg("Starting estafette-gke-node-pool-shifter...")

	kubernetes, err := NewKubernetesClient(os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT"),
		os.Getenv("KUBERNETES_NAMESPACE"), *kubeConfigPath)

	if err != nil {
		log.Fatal().Err(err).Msg("Error initializing Kubernetes client")
	}

	// start prometheus
	go func() {
		log.Info().
			Str("port", *prometheusAddress).
			Str("path", *prometheusMetricsPath).
			Msg("Serving Prometheus metrics...")

		http.Handle(*prometheusMetricsPath, promhttp.Handler())

		if err := http.ListenAndServe(*prometheusAddress, nil); err != nil {
			log.Fatal().Err(err).Msg("Starting Prometheus listener failed")
		}
	}()

	// create GCloud Client
	gcloud, err := NewGCloudClient()

	if err != nil {
		log.Fatal().Err(err).Msg("Error creating GCloud client")
	}

	// get project information (gcloud project, zone and cluster id) from one of the node
	nodes, err := kubernetes.GetNodeList("")

	if err != nil {
		log.Fatal().Err(err).Msg("Error while getting the list of nodes")
	}

	if len(nodes.Items) == 0 {
		log.Fatal().Msg("Error there is no node in the cluster")
	}

	gcloud.GetProjectDetailsFromNode(*nodes.Items[0].Spec.ProviderID)

	if err != nil {
		log.Fatal().Err(err).Msg("Error getting project details from node")
	}

	// now that we have the cluster id, create GCloud container client
	gcloudContainerClient, err := gcloud.NewGCloudContainerClient()

	if err != nil {
		log.Fatal().Err(err).Msg("Error creating GCloud container client")
	}

	// define channel and wait group to gracefully shutdown the application
	gracefulShutdown := make(chan os.Signal)
	signal.Notify(gracefulShutdown, syscall.SIGTERM, syscall.SIGINT)
	waitGroup := &sync.WaitGroup{}

	// process node pool
	go func(waitGroup *sync.WaitGroup) {
		for {
			log.Info().Msg("Checking node pool to shift...")

			// interval between each process
			processInterval := ApplyJitter(*interval)

			// interval between actions, leverage provider requests when
			// another operation is already operating on the cluster
			actionInterval := ApplyJitter(10)

			nodesFrom, err := kubernetes.GetNodeList(*nodePoolFrom)

			if err != nil {
				log.Error().
					Err(err).
					Str("node-pool", *nodePoolFrom).
					Msg("Error while getting the list of nodes")
				log.Info().Msgf("Sleeping for %v seconds...", processInterval)
				time.Sleep(time.Duration(processInterval) * time.Second)
				continue
			}

			nodesTo, err := kubernetes.GetNodeList(*nodePoolTo)

			if err != nil {
				log.Error().
					Err(err).
					Str("node-pool", *nodePoolTo).
					Msg("Error while getting the list of nodes")
				log.Info().Msgf("Sleeping for %v seconds...", processInterval)
				time.Sleep(time.Duration(processInterval) * time.Second)
				continue
			}

			nodePoolFromSize := len(nodesFrom.Items)

			log.Info().
				Str("node-pool", *nodePoolFrom).
				Msgf("Node pool has %d node(s), minimun wanted: %d node(s)", nodePoolFromSize, *nodePoolFromMinNode)

			// TODO remove nodePoolFromMinNode, use value from node pool autoscaling setting (min node) instead
			if nodePoolFromSize > *nodePoolFromMinNode && len(nodesFrom.Items) > 0 {
				log.Info().
					Str("node-pool", *nodePoolTo).
					Msg("Attempting to shift one node...")

				status := "shifted"

				waitGroup.Add(1)
				if err := shiftNode(gcloudContainerClient, *nodePoolFrom, *nodePoolTo, nodesFrom, nodesTo); err != nil {
					status = "failed"
				}
				waitGroup.Done()

				nodeTotals.With(prometheus.Labels{"status": status}).Inc()

				log.Info().Msgf("Sleeping for %v seconds...", actionInterval)
				time.Sleep(time.Duration(actionInterval) * time.Second)
			} else {
				log.Info().Msgf("Sleeping for %v seconds...", processInterval)
				time.Sleep(time.Duration(processInterval) * time.Second)
			}
		}
	}(waitGroup)

	signalReceived := <-gracefulShutdown
	log.Info().
		Msgf("Received signal %v. Sending shutdown and waiting on goroutines...", signalReceived)

	waitGroup.Wait()

	log.Info().Msg("Shutting down...")
}

// shiftNode safely try to add a new node to a pool then remove a node from another
func shiftNode(g GCloudContainerClient, fromName, toName string, from, to *apiv1.NodeList) (err error) {
	// Add node
	toCurrentSize := len(to.Items)
	toNewSize := int64(toCurrentSize + 1)

	log.Info().
		Str("node-pool", toName).
		Msgf("Adding 1 node to the pool, currently %d node(s), expecting %d node(s)", toCurrentSize, toNewSize)

	err = g.SetNodePoolSize(toName, toNewSize)

	if err != nil {
		log.Error().
			Err(err).
			Str("node-pool", toName).
			Msg("Error resizing node pool")
		return
	}

	// Remove node
	fromCurrentSize := len(from.Items)
	fromNewSize := int64(fromCurrentSize - 1)

	log.Info().
		Str("node-pool", fromName).
		Msgf("Removing 1 node from the pool, currently %d node(s), expecting %d node(s)", fromCurrentSize, fromNewSize)

	err = g.SetNodePoolSize(fromName, fromNewSize)

	if err != nil {
		log.Error().
			Err(err).
			Str("node-pool", fromName).
			Msg("Error resizing node pool")
	}

	return
}
