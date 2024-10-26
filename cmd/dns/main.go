package main

import (
	"flag"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"log"
	"os"
	"strings"
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

func podChange(net *Networks, pod *corev1.Pod) {
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
		if len(networks) > 1 {
			log.Printf("Pod %s/%s has more than one network, this is currently not supported",
				pod.Namespace, pod.Name)
		}
		network := networks[0]

		podObj := Pod{
			IP:          IPAddress(pod.Status.PodIP),
			Namespace:   pod.Namespace,
			Name:        pod.Name,
			HostAliases: hostaliases,
			Network:     network,
		}
		err := net.Add(&podObj)
		if err != nil {
			log.Printf("Error adding pod %s/%s: %v", pod.Namespace, pod.Name, err)
		}
		net.LogNetworks()
	}
}

func podDeletion(net *Networks, pod *corev1.Pod) {
	log.Printf("delete: %s/%s %s", pod.ObjectMeta.Namespace,
		pod.ObjectMeta.Name, pod.Status.PodIP)
	net.Delete(IPAddress(pod.Status.PodIP))
	net.LogNetworks()
}

func main() {
	flag.Parse()

	// Using First sample from https://pkg.go.dev/k8s.io/client-go/tools/clientcmd to automatically deal with environment variables and default file paths

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	// if you want to change the loading rules (which files in which order), you can do so here

	configOverrides := &clientcmd.ConfigOverrides{}
	// if you want to change override values or bind them to flags, there are methods to help you

	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeConfig.ClientConfig()
	if err != nil {
		log.Panicln(err.Error())
	}

	// Note that this *should* automatically sanitize sensitive fields
	log.Println("Using configuration:", config.String())

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Panicln(err.Error())
	}

	namespace, _, err := kubeConfig.Namespace()
	if err != nil {
		log.Panicf("Could not get namespace")
	}

	log.Printf("Watching namespace %s", namespace)

	networks := NewNetworks()

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
				podChange(networks, getPod(obj))
			},
			UpdateFunc: func(_ any, obj any) {
				podChange(networks, getPod(obj))
			},
			DeleteFunc: func(obj any) {
				podDeletion(networks, getPod(obj))
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
