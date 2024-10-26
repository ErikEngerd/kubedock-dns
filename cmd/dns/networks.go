package main

import (
	"fmt"
	"log"
	"slices"
)

type IPAddress string
type Hostname string
type NetworkId string
type PodName string

type Pod struct {
	IP          IPAddress
	Namespace   string
	Name        string
	HostAliases []Hostname
	Network     NetworkId
}

type Network struct {
	Id             NetworkId
	IPToPod        map[IPAddress]*Pod
	HostAliasToPod map[Hostname]*Pod
}

func NewNetwork(id NetworkId) *Network {
	network := Network{
		Id:             id,
		IPToPod:        make(map[IPAddress]*Pod),
		HostAliasToPod: make(map[Hostname]*Pod),
	}
	return &network
}

func (net *Network) Add(pod *Pod) error {
	for _, hostAlias := range pod.HostAliases {
		existingPod := net.HostAliasToPod[hostAlias]
		if existingPod != nil {
			return fmt.Errorf("Pod %+v has hostAlias %s which is already mapped to another pod %+v",
				pod, hostAlias, existingPod)
		}
	}

	net.IPToPod[pod.IP] = pod
	for _, hostAlias := range pod.HostAliases {
		net.HostAliasToPod[hostAlias] = pod
	}
	return nil
}

// Networks is not thread-safe and is meant to be used using copy-on-write
// This make the design a lot easier since it will support many change scenario's
// out of the box.
type Networks struct {
	NameToNetwork map[NetworkId]*Network
	IpToNetwork   map[IPAddress]*Network
}

func NewNetworks() *Networks {
	return &Networks{
		NameToNetwork: make(map[NetworkId]*Network),
		IpToNetwork:   make(map[IPAddress]*Network),
	}
}

func (net *Networks) Add(pod *Pod) error {
	if pod.IP == "" {
		log.Panicf("Pod IP is not set: %+v", pod)
	}
	if pod.Network == "" {
		log.Panicf("Pod network is not set: %+v", pod)
	}
	// is there a network that contains the pod ip?
	network := net.NameToNetwork[pod.Network]
	if network == nil {
		network = NewNetwork(pod.Network)
	}

	// is there a Pod already registered for htis IP, and if so, is it the
	// same pod
	existingPod := network.IPToPod[pod.IP]
	log.Printf("Existing pod %+v", existingPod)
	if existingPod != nil {
		if existingPod.Namespace == existingPod.Namespace && existingPod.Name == pod.Name {
			// looks like same pod: hostAliases and network Id must be the same
			if !slices.Equal(existingPod.HostAliases, pod.HostAliases) {
				log.Panicf("Pod %+v registered twice with different hostaliases", pod)
			}
			if existingPod.Network != pod.Network {
				log.Panicf("Pod %+v registered twice with different network", pod)
			}
		} else {
			// another pod with te same IP? This should not happen.
			log.Panicf("Two pods with same ip: %+v and %+v", pod, existingPod)
		}
		return nil
	}

	err := network.Add(pod)
	if err != nil {
		return err
	}

	net.IpToNetwork[pod.IP] = network
	net.NameToNetwork[pod.Network] = network

	return nil
}

func (net *Networks) LogNetworks() {
	log.Printf("Network count: %d", len(net.IpToNetwork))
	for networkId, network := range net.NameToNetwork {
		log.Printf("Network %s", networkId)
		for ip, pod := range network.IPToPod {
			log.Printf("  Pod: %s/%s", pod.Namespace, pod.Name)
			log.Printf("    IP: %s", ip)
			for _, hostAlias := range pod.HostAliases {
				log.Printf("    Hostalias: %s", hostAlias)
			}
			log.Println()
		}
	}

	for networkId, network := range net.NameToNetwork {
		log.Printf("Network %s %v", networkId, network)
	}

}

func (net *Networks) Lookup(sourceIp IPAddress, hostname Hostname) IPAddress {
	network := net.IpToNetwork[sourceIp]
	if network == nil {
		return ""
	}
	pod := network.HostAliasToPod[hostname]
	if pod == nil {
		return ""
	}
	return pod.IP
}

func (net *Networks) ReverseLookup(sourceIp IPAddress, ip IPAddress) []Hostname {
	network := net.IpToNetwork[sourceIp]
	if network == nil {
		return nil
	}
	pod := network.IPToPod[ip]
	if pod == nil {
		return nil
	}
	return pod.HostAliases
}

type Pods struct {
	// maps pod namespace/name to pod
	Pods map[string]*Pod
}

func NewPods() *Pods {
	return &Pods{
		Pods: make(map[string]*Pod),
	}
}

func (pods *Pods) AddOrUpdate(pod *Pod) {
	pods.Pods[pod.Namespace+"/"+pod.Name] = pod
}

func (pods *Pods) Delete(namespace, name string) {
	delete(pods.Pods, namespace+"/"+name)
}

func (pods *Pods) Networks() (*Networks, error) {
	networks := NewNetworks()
	for _, pod := range pods.Pods {
		err := networks.Add(pod)
		if err != nil {
			return nil, err
		}
	}
	return networks, nil
}
