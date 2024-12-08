package watcher

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"wamblee.org/kubedock/dns/internal/config"
	"wamblee.org/kubedock/dns/internal/model"
)

type PodAdmin interface {
	AddOrUpdate(pod *model.Pod)
	Delete(namespace, name string)
}

func WatchPods(
	clientset *kubernetes.Clientset,
	namespace string,
	pods PodAdmin,
	podConfig config.PodConfig) {

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
			pod, err := model.GetPodEssentials(k8spod, "", podConfig)
			if err == nil {
				pods.AddOrUpdate(pod)
			} else {
				klog.Infof("Ignoring pod %s/%s: %v", k8spod.Namespace, k8spod.Name, err)
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
		klog.Fatalf("Object of wrong type: %v", obj)
	}
	return k8spod
}
