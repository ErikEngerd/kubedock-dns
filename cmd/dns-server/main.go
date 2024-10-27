package main

import (
	"flag"
	"github.com/miekg/dns"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/cache"
	"log"
	"os"
	"strings"
	"wamblee.org/kubedock/dns/internal/support"
)

var KUBEDOCK_HOSTALIAS_PREFIX = "kubedock.hostalias/"
var KUBEDOCK_NETWORKID_PREFIX = "kubedock.network/"

var (
	ignoreNormal = flag.Bool("ignore-normal", false, "ignore events of type 'Normal' to reduce noise")
)

func getPod(obj any) *corev1.Pod {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		log.Panicf("Object of wrong type: %v", obj)
		os.Exit(1)
	}
	return pod
}

func podChange(pods *Pods, dns *KubeDockDns, pod *corev1.Pod) {
	//j, _ := json.Marshal(obj)
	//log.Printf("%s\n", string(j))

	log.Printf("create/update: %s/%s %s", pod.ObjectMeta.Namespace,
		pod.ObjectMeta.Name, pod.Status.PodIP)

	if pod.Status.PodIP != "" {
		// gather hostaliases and network from the annotations
		networks := make([]NetworkId, 0)
		hostaliases := make([]Hostname, 0)

		for key, value := range pod.Annotations {
			if strings.HasPrefix(key, KUBEDOCK_HOSTALIAS_PREFIX) {
				hostaliases = append(hostaliases, Hostname(value))
			} else if strings.HasPrefix(key, KUBEDOCK_NETWORKID_PREFIX) {
				networks = append(networks, NetworkId(value))
			}
		}

		log.Printf("Pod %s/%s: hostaliases %v, networks %v",
			pod.Namespace, pod.Name, hostaliases, networks)
		if len(networks) == 0 || len(hostaliases) == 0 {
			log.Printf("Pod %s/%s not configured in DNS",
				pod.Namespace, pod.Name)
			return
		}

		podObj := Pod{
			IP:          IPAddress(pod.Status.PodIP),
			Namespace:   pod.Namespace,
			Name:        pod.Name,
			HostAliases: hostaliases,
			Networks:    networks,
		}
		pods.AddOrUpdate(&podObj)
		net, err := pods.Networks()
		if err != nil {
			log.Printf("Error adding pod %s/%s: %v", pod.Namespace, pod.Name, err)
			return
		}
		dns.SetNetworks(net)
		net.LogNetworks()
	}
}

func podDeletion(pods *Pods, dns *KubeDockDns, pod *corev1.Pod) {
	log.Printf("delete: %s/%s %s", pod.ObjectMeta.Namespace,
		pod.ObjectMeta.Name, pod.Status.PodIP)
	pods.Delete(pod.Namespace, pod.Name)
	net, err := pods.Networks()
	if err != nil {
		log.Printf("Error deleting pod %v: %v", pod, err)
		return
	}
	dns.SetNetworks(net)
	net.LogNetworks()
}

func createDns() *KubeDockDns {
	clientConfig, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil {
		panic(err)
	}
	upstreamDnsServer := clientConfig.Servers[0]
	log.Printf("DNS server %s", upstreamDnsServer)
	kubedocDns := NewKubeDockDns(upstreamDnsServer+":53", ":1053")
	return kubedocDns
}

func main() {
	flag.Parse()

	clientset, namespace := support.GetKubernetesConnection()

	log.Printf("Watching namespace %s", namespace)

	pods := NewPods()
	dns := createDns()
	sourceIp := os.Getenv("KUBEDOCK_DNS_SOURCE_IP")
	if sourceIp != "" {
		dns.OverrideSourceIP(IPAddress(sourceIp))
	}

	go func() {
		dns.Serve()
	}()

	watchlist := cache.NewListWatchFromClient(
		clientset.CoreV1().RESTClient(),
		"pods",
		namespace,
		fields.Everything(),
	)

	options := cache.InformerOptions{
		ListerWatcher: watchlist,
		ObjectType:    &corev1.Pod{},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				podChange(pods, dns, getPod(obj))
			},
			UpdateFunc: func(_ any, obj any) {
				podChange(pods, dns, getPod(obj))
			},
			DeleteFunc: func(obj any) {
				podDeletion(pods, dns, getPod(obj))
			},
		},
		ResyncPeriod: 0,
	}

	_, controller := cache.NewInformerWithOptions(options)

	stop := make(chan struct{})
	defer close(stop)
	go controller.Run(stop)
	select {}
}
