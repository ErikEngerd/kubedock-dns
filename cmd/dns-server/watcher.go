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

// TODO serialize updates to the administration using channels

func WatchPods(
	clientset *kubernetes.Clientset,
	namespace string,
	pods PodAdmin) {

	serializer := make(chan func())
	go func() {
		for action := range serializer {
			action()
		}
	}()

	watchlist := cache.NewListWatchFromClient(
		clientset.CoreV1().RESTClient(),
		"pods",
		namespace,
		fields.Everything(),
	)

	addOrUpdate := func(obj interface{}) {
		serializer <- func() {
			k8spod := getPod(obj)
			pod, err := getPodEssentials(k8spod, "")
			if err == nil {
				pods.AddOrUpdate(pod)
			} else {
				log.Printf("Ignoring pod %s/%s: %v", k8spod.Namespace, k8spod.Name, err)
			}
		}
	}

	options := cache.InformerOptions{
		ListerWatcher: watchlist,
		ObjectType:    &corev1.Pod{},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: addOrUpdate,
			UpdateFunc: func(_ any, obj any) {
				addOrUpdate(obj)
			},
			DeleteFunc: func(obj any) {
				pod := getPod(obj)
				serializer <- func() {
					pods.Delete(pod.Namespace, pod.Name)
				}
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
		return nil, fmt.Errorf("%s/%s: Pod does not have an IP",
			k8spod.Namespace, k8spod.Name)
	}

	if k8spod.Labels[KUBEDOCK_LABEL_NAME] != "true" {
		return nil, fmt.Errorf("%s/%s: Pod does not have label %s set to 'true'",
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
		return nil, fmt.Errorf("%s/%s: Pod not configured in DNS",
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
