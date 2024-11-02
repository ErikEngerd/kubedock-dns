package main

import (
	"context"
	goflags "flag"
	"fmt"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
	"os"
	"wamblee.org/kubedock/dns/internal/support"
)

func createDns() *KubeDockDns {
	clientConfig := support.GetClientConfig()
	upstreamDnsServer := NewExternalDNSServer(clientConfig.Servers[0] + ":53")
	klog.Infof("Upstream DNS server %s", upstreamDnsServer)
	kubedocDns := NewKubeDockDns(upstreamDnsServer, ":1053", clientConfig.Search[0])
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

func execute(cmd *cobra.Command, args []string) error {

	klog.Info("Starting DNS server and mutator")
	klog.V(2).Info("Verbose logging enabled")
	klog.V(3).Info("Debug logging enabled")

	if len(args) > 0 {
		return fmt.Errorf("No arguments expected, only options")
	}
	fmt.Printf("Host alias prefix: %s\n", KUBEDOCK_HOSTALIAS_PREFIX)
	fmt.Printf("Network prefix:    %s\n", KUBEDOCK_NETWORKID_PREFIX)
	fmt.Printf("Pod label:         %s\n", KUBEDOCK_LABEL_NAME)
	fmt.Printf("CRT file:          %s\n", KUBEDOCK_CRT_FILE)
	fmt.Printf("KEY file:          %s\n", KUBEDOCK_KEY_FILE)

	ctx := context.Background()

	clientset, namespace := support.GetKubernetesConnection()

	klog.Infof("Watching namespace %s", namespace)

	// DNS server
	dns := createDns()
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
	go WatchPods(clientset, namespace, dnsWatcherIntegration)

	// Admission controller

	if err := runAdmisstionController(ctx, pods, clientset, namespace, "dns-server",
		KUBEDOCK_CRT_FILE, KUBEDOCK_KEY_FILE); err != nil {
		return fmt.Errorf("Could not start admission controler: %+v", err)
	}
	return nil
}

func main() {
	klogFlags := goflags.NewFlagSet("", goflags.PanicOnError)
	klog.InitFlags(klogFlags)

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
		RunE: execute,
	}

	cmd.PersistentFlags().StringVar(&KUBEDOCK_HOSTALIAS_PREFIX, "host-alias-prefix",
		"kubedock.hostalias/", "annotation prefix for hosttnames. ")
	cmd.PersistentFlags().StringVar(&KUBEDOCK_NETWORKID_PREFIX, "network-prefix",
		"kubedock.network/", "annotation prefix for network names. ")
	cmd.PersistentFlags().StringVar(&KUBEDOCK_LABEL_NAME, "label-name",
		"kubedock-pod", "name of the label (with value 'true') to be applied to pods")
	cmd.PersistentFlags().StringVar(&KUBEDOCK_CRT_FILE, "cert",
		"/etc/kubedock/pki/tls.crt", "Certificate file")
	cmd.PersistentFlags().StringVar(&KUBEDOCK_KEY_FILE, "key",
		"/etc/kubedock/pki/tls.key", "Key file")

	cmd.Flags().AddGoFlagSet(klogFlags)

	cmd.Execute()
}
