package main

import (
	"context"
	"flag"
	"log"
	"os"
	"wamblee.org/kubedock/dns/internal/support"
)

var KUBEDOCK_HOSTALIAS_PREFIX = "kubedock.hostalias/"
var KUBEDOCK_NETWORKID_PREFIX = "kubedock.network/"

var (
	ignoreNormal = flag.Bool("ignore-normal", false, "ignore events of type 'Normal' to reduce noise")
)

func createDns() *KubeDockDns {
	clientConfig := support.GetClientConfig()
	upstreamDnsServer := NewExternalDNSServer(clientConfig.Servers[0] + ":53")
	log.Printf("DNS server %s", upstreamDnsServer)
	kubedocDns := NewKubeDockDns(upstreamDnsServer, ":1053", clientConfig.Search[0])
	return kubedocDns
}

type DnsWatcherIntegration struct {
	pods *Pods
	dns  *KubeDockDns
}

func (integrator *DnsWatcherIntegration) AddOrUpdate(pod *Pod) {
	log.Printf("Pod added or updated %+v", *pod)
	if integrator.pods.AddOrUpdate(pod) {
		integrator.updateDns()
	}
}

func (integrator *DnsWatcherIntegration) Delete(namespace, name string) {
	integrator.pods.Delete(namespace, name)
	integrator.updateDns()
}

func (integrator *DnsWatcherIntegration) updateDns() {
	networks, err := integrator.pods.Networks()
	// TODO robusness: when one pod  results in an error we don't want
	// the hwole network configuration ot freeze of be incomplete
	if err != nil {
		log.Printf("Errors occured creating network configuration '%v'", err)
		return
	}
	integrator.dns.SetNetworks(networks)
	networks.Log()
}

func main() {
	flag.Parse()

	ctx := context.Background()

	clientset, namespace := support.GetKubernetesConnection()

	log.Printf("Watching namespace %s", namespace)

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
		"/etc/kubedock/pki/tls.crt", "/etc/kubedock/pki/tls.key"); err != nil {
		log.Panicf("Could not start admission controler: %+v", err)
	}
}
