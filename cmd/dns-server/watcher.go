package main

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"log"
	"strings"
)

type PodAdmin interface {
	AddOrUpdate(pod *Pod)
	Delete(namespace, name string)
}

func WatchPods(
	clientset *kubernetes.Clientset,
	namespace string,
	pods PodAdmin) {

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
				pod, err := getPodEssentials(getPod(obj), "")
				if err == nil {
					pods.AddOrUpdate(pod)
				}
			},
			UpdateFunc: func(_ any, obj any) {
				pod, err := getPodEssentials(getPod(obj), "")
				if err == nil {
					pods.AddOrUpdate(pod)
				}
			},
			DeleteFunc: func(obj any) {
				pod := getPod(obj)
				pods.Delete(pod.Namespace, pod.Name)
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

func getPod(obj any) *corev1.Pod {
	k8spod, ok := obj.(*corev1.Pod)
	if !ok {
		log.Panicf("Object of wrong type: %v", obj)
	}
	return k8spod
}

func getPodEssentials(k8spod *corev1.Pod, overrideIP string) (*Pod, error) {

	if overrideIP == "" && k8spod.Status.PodIP == "" {
		return nil, fmt.Errorf("Pod does not have an IP: %s/%s",
			k8spod.Namespace, k8spod.Name)
	}

	if k8spod.Labels[KUBEDOCK_LABEL_NAME] != "true" {
		return nil, fmt.Errorf("Pod %s/%s does not have label %s set to 'true'",
			k8spod.Namespace, k8spod.Name, KUBEDOCK_LABEL_NAME)
	}

	podIP := k8spod.Status.PodIP
	if overrideIP != "" {
		podIP = overrideIP
	}

	networks := make([]NetworkId, 0)
	hostaliases := make([]Hostname, 0)

	for key, value := range k8spod.Annotations {
		if strings.HasPrefix(key, KUBEDOCK_HOSTALIAS_PREFIX) {
			hostaliases = append(hostaliases, Hostname(value))
		} else if strings.HasPrefix(key, KUBEDOCK_NETWORKID_PREFIX) {
			networks = append(networks, NetworkId(value))
		}
	}

	log.Printf("Pod %s/%s: hostaliases %v, networks %v",
		k8spod.Namespace, k8spod.Name, hostaliases, networks)
	if len(networks) == 0 || len(hostaliases) == 0 {
		return nil, fmt.Errorf("Pod %s/%s not configured in DNS",
			k8spod.Namespace, k8spod.Name)
	}

	pod := Pod{
		IP:          IPAddress(podIP),
		Namespace:   k8spod.Namespace,
		Name:        k8spod.Name,
		HostAliases: hostaliases,
		Networks:    networks,
	}

	return &pod, nil
}
