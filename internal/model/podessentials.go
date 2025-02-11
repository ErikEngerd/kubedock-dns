package model

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"strings"
	"wamblee.org/kubedock/dns/internal/config"
)

func GetPodEssentials(k8spod *corev1.Pod, overrideIP string,
	podConfig config.PodConfig) (*Pod, error) {

	if overrideIP == "" && k8spod.Status.PodIP == "" {
		return nil, fmt.Errorf("%s/%s: Pod does not have an IP (yet)",
			k8spod.Namespace, k8spod.Name)
	}

	if k8spod.Labels[podConfig.LabelName] != "true" {
		return nil, fmt.Errorf("%s/%s: Pod does not have label %s set to 'true'",
			k8spod.Namespace, k8spod.Name, podConfig.LabelName)
	}

	podIP := k8spod.Status.PodIP
	if overrideIP != "" {
		podIP = overrideIP
	}

	networks := make([]NetworkId, 0)
	hostaliases := make([]Hostname, 0)

	for key, value := range k8spod.Annotations {
		if strings.HasPrefix(key, podConfig.HostAliasPrefix) {
			hostaliases = append(hostaliases, Hostname(value))
		} else if strings.HasPrefix(key, podConfig.NetworkIdPrefix) {
			networks = append(networks, NetworkId(value))
		}
	}

	klog.Infof("%s/%s: hostaliases %v, networks %v",
		k8spod.Namespace, k8spod.Name, hostaliases, networks)
	if len(networks) == 0 || len(hostaliases) == 0 {
		return nil, fmt.Errorf("%s/%s: Pod not configured in DNS, either no host or no network defined",
			k8spod.Namespace, k8spod.Name)
	}

	ready := false
	for _, condition := range k8spod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			ready = true
		}
	}
	if k8spod.DeletionTimestamp != nil {
		ready = false
	}

	pod, err := NewPod(
		IPAddress(podIP),
		k8spod.Namespace,
		k8spod.Name,
		hostaliases,
		networks,
		ready,
	)

	return pod, err
}
