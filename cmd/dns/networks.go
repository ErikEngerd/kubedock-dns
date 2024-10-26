package main

import (
	"fmt"
	"log"
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
	Networks    []NetworkId
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
		if existingPod != nil && !(existingPod.Namespace == pod.Namespace && existingPod.Name == pod.Name) {
			return fmt.Errorf("Pod %+v has hostAlias %s in network %s which is already mapped to another pod %+v",
				pod, hostAlias, net.Id, existingPod)
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

type NetworkMap map[NetworkId]*Network

type Networks struct {
	NameToNetwork NetworkMap
	IpToNetworks  map[IPAddress]NetworkMap
}

func NewNetworks() *Networks {
	return &Networks{
		NameToNetwork: make(NetworkMap),
		IpToNetworks:  make(map[IPAddress]NetworkMap),
	}
}

func (net *Networks) Add(pod *Pod) error {
	if pod.IP == "" {
		log.Panicf("Pod IP is not set: %+v", pod)
	}
	if len(pod.Networks) == 0 {
		log.Panicf("Pod networks are not set: %+v", pod)
	}

	for _, networkId := range pod.Networks {
		// is there a network that contains the pod ip?
		network := net.NameToNetwork[networkId]
		if network == nil {
			network = NewNetwork(networkId)
		}
		if net.IpToNetworks[pod.IP] == nil {
			net.IpToNetworks[pod.IP] = make(NetworkMap)
		}
		net.IpToNetworks[pod.IP][networkId] = network
		net.NameToNetwork[networkId] = network
		err := network.Add(pod)
		if err != nil {
			return err
		}
	}

	return nil
}

func (net *Networks) LogNetworks() {
	log.Printf("Network count: %d", len(net.NameToNetwork))
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
	log.Printf("Lookup source ip %s host %s", sourceIp, hostname)
	networks := net.IpToNetworks[sourceIp]
	log.Printf("Networks: %+v", net)
	if networks == nil {
		return ""
	}
	for _, network := range networks {
		log.Printf("Examining network %s", network.Id)
		pod := network.HostAliasToPod[hostname]
		if pod != nil {
			return pod.IP
		}
	}
	return ""
}

func (net *Networks) ReverseLookup(sourceIp IPAddress, ip IPAddress) []Hostname {
	log.Printf("ReverseLookup: sourceIP %s IP %s", sourceIp, ip)
	networks := net.IpToNetworks[sourceIp]
	if networks == nil {
		return nil
	}
	for _, network := range networks {
		log.Printf("Trying %s %v", network.Id, network)
		pod := network.IPToPod[ip]
		if pod != nil {
			log.Printf("Found hostaliases %v", pod.HostAliases)
			return pod.HostAliases
		}
	}
	return nil
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
		log.Printf("Adding pod %s/%s", pod.Namespace, pod.Name)
		err := networks.Add(pod)
		if err != nil {
			return nil, err
		}
	}
	return networks, nil
}
