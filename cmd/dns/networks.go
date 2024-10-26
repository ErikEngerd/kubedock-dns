package main

import (
	"fmt"
	"log"
	"slices"
	"sync"
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

func (net *Network) Delete(ip IPAddress) {
	pod := net.IPToPod[ip]
	if pod == nil {
		return
	}
	delete(net.IPToPod, pod.IP)
	for _, hostAlias := range pod.HostAliases {
		delete(net.HostAliasToPod, hostAlias)
	}
}

type Networks struct {
	mutex         sync.RWMutex
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
	net.mutex.Lock()
	defer net.mutex.Unlock()

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

func (net *Networks) Delete(ip IPAddress) {
	net.mutex.Lock()
	defer net.mutex.Unlock()

	network := net.IpToNetwork[ip]
	if network == nil {
		log.Printf("delete: IP %s is not in any network", ip)
		return
	}
	network.Delete(ip)

	delete(net.IpToNetwork, ip)
	if len(network.IPToPod) == 0 {
		log.Printf("No more pods in network %s, deleting it", network.Id)
		delete(net.NameToNetwork, network.Id)
	}
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
	net.mutex.RLock()
	defer net.mutex.RUnlock()

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
	net.mutex.RLock()
	defer net.mutex.RUnlock()

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
