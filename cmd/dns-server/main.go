package main

import (
	"context"
	goflags "flag"
	"fmt"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
	"os"
	"time"
	"wamblee.org/kubedock/dns/internal/support"
)

func createDns(config Config) *KubeDockDns {
	clientConfig := support.GetClientConfig()
	clientConfig.Timeout = int(config.DnsTimeout.Seconds())
	clientConfig.Attempts = config.DnsRetries

	upstreamDnsServer := NewExternalDNSServer(clientConfig.Servers[0] + ":53")
	klog.Infof("Upstream DNS server %s", upstreamDnsServer)
	kubedocDns := NewKubeDockDns(upstreamDnsServer, ":1053", clientConfig.Search[0],
		config.InternalDomains)
	return kubedocDns
}

type DnsWatcherIntegration struct {
	pods *Pods
	dns  *KubeDockDns
}

func (integrator *DnsWatcherIntegration) AddOrUpdate(pod *Pod) {
	klog.V(2).Infof("%v/%v: Pod added or updated", pod.Namespace, pod.Name)
	if integrator.pods.AddOrUpdate(pod) {
		integrator.updateDns()
	}
}

func (integrator *DnsWatcherIntegration) Delete(namespace, name string) {
	klog.V(2).Infof("%v/%v: deleted", namespace, name)
	integrator.pods.Delete(namespace, name)
	integrator.updateDns()
}

func (integrator *DnsWatcherIntegration) updateDns() {
	networks, err := integrator.pods.Networks()
	if err != nil {
		klog.Warningf("Errors occured creating network configuration, only conflicting pods are affected '%v'", err)
	}
	integrator.dns.SetNetworks(networks)
	if klog.V(3).Enabled() {
		networks.Log()
	}
}

func execute(cmd *cobra.Command, args []string, config Config) error {

	klog.Info("Starting DNS server and mutator")
	klog.V(2).Info("Verbose logging enabled")
	klog.V(3).Info("Debug logging enabled")

	if len(args) > 0 {
		return fmt.Errorf("No arguments expected, only options")
	}
	fmt.Printf("Host alias prefix:  %s\n", config.PodConfig.HostAliasPrefix)
	fmt.Printf("Network prefix:     %s\n", config.PodConfig.NetworkIdPrefix)
	fmt.Printf("Pod label:          %s\n", config.PodConfig.LabelName)
	fmt.Printf("CRT file:           %s\n", config.CrtFile)
	fmt.Printf("KEY file:           %s\n", config.KeyFile)
	fmt.Printf("Client DNS timeout: %v\n", config.DnsTimeout)
	fmt.Printf("Client DNS retries: %v\n", config.DnsRetries)

	ctx := context.Background()

	clientset, namespace := support.GetKubernetesConnection()

	klog.Infof("Watching namespace %s", namespace)

	// DNS server
	dns := createDns(config)
	sourceIp := os.Getenv("KUBEDOCK_DNS_SOURCE_IP")
	if sourceIp != "" {
		dns.OverrideSourceIP(IPAddress(sourceIp))
	}
	go func() {
		dns.Serve()
	}()

	// pod administration
	pods := NewPods()
	dnsWatcherIntegration := &DnsWatcherIntegration{
		pods: pods,
		dns:  dns,
	}

	// Watching Pods
	go WatchPods(clientset, namespace, dnsWatcherIntegration, config.PodConfig)

	// Admission controller

	if err := runAdmisstionController(ctx, pods, clientset, namespace, "dns-server",
		config.CrtFile, config.KeyFile, config.PodConfig); err != nil {
		return fmt.Errorf("Could not start admission controler: %+v", err)
	}
	return nil
}

func main() {
	klogFlags := goflags.NewFlagSet("", goflags.PanicOnError)
	klog.InitFlags(klogFlags)

	config := Config{}
	cmd := &cobra.Command{
		Use:   "kubedock-dns",
		Short: "Run a DNS server and mutator for test containers",
		Long: `
Run a DNS server and mutator for test containers. 
By labeling PODs with the host aliases and networks, 
this provides separate networks of communicating pods
in a single namespace. Thus emulating a typical docker
setup with host aliases where some containers share a 
network`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return execute(cmd, args, config)
		},
	}

	cmd.PersistentFlags().StringVar(&config.PodConfig.HostAliasPrefix, "host-alias-prefix",
		"kubedock.hostalias/", "annotation prefix for hosttnames. ")
	cmd.PersistentFlags().StringVar(&config.PodConfig.NetworkIdPrefix, "network-prefix",
		"kubedock.network/", "annotation prefix for network names. ")
	cmd.PersistentFlags().StringVar(&config.PodConfig.LabelName, "label-name",
		"kubedock", "name of the label (with value 'true') to be applied to pods")
	cmd.PersistentFlags().StringVar(&config.CrtFile, "cert",
		"/etc/kubedock/pki/tls.crt", "Certificate file")
	cmd.PersistentFlags().StringVar(&config.KeyFile, "key",
		"/etc/kubedock/pki/tls.key", "Key file")
	cmd.PersistentFlags().StringSliceVar(&config.InternalDomains,
		"internal-domain", []string{}, "internal domains that will not be resolved using the upstream DNS server.\n"+
			"By default empty so that only domain names without dots in them are considered to be internal")
	cmd.PersistentFlags().DurationVar(&config.DnsTimeout, "client-dns-timeout",
		30*time.Second, "DNS timeout to use by instrumented pods")
	cmd.PersistentFlags().IntVar(&config.DnsRetries, "client-dns-retries",
		5, "Max DNS retries to do by clients")
	cmd.Flags().AddGoFlagSet(klogFlags)

	cmd.Execute()
}
