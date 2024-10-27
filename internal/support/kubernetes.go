package support

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"log"
)

func GetKubernetesConnection() (*kubernetes.Clientset, string) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeConfig.ClientConfig()
	if err != nil {
		log.Panicln(err.Error())
	}

	log.Println("Using configuration:", config.String())

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Panicln(err.Error())
	}

	namespace, _, err := kubeConfig.Namespace()
	if err != nil {
		log.Panicf("Could not get namespace")
	}
	return clientset, namespace
}
