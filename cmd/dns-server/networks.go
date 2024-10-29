package main

import (
	"errors"
	"fmt"
	"log"
	"reflect"
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
		// does the pod network already exist?
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

func (net *Networks) Log() {
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

func (net *Networks) Lookup(sourceIp IPAddress, hostname Hostname) []IPAddress {
	log.Printf("Lookup source ip '%s' host '%s'", sourceIp, hostname)
	networks := net.IpToNetworks[sourceIp]
	log.Printf("Networks: %+v", net.IpToNetworks)
	if networks == nil {
		return make([]IPAddress, 0)
	}
	log.Printf("No networks: %d", len(networks))
	res := make([]IPAddress, 0)
	for _, network := range networks {
		log.Printf("Examining network %s", network.Id)
		pod := network.HostAliasToPod[hostname]
		if pod != nil {
			res = append(res, pod.IP)
		}
	}
	return res
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
	mutex sync.RWMutex
	// maps pod namespace/name to pod
	Pods map[string]*Pod
}

func NewPods() *Pods {
	return &Pods{
		mutex: sync.RWMutex{},
		Pods:  make(map[string]*Pod),
	}
}

func (pods *Pods) AddOrUpdate(pod *Pod) bool {
	pods.mutex.Lock()
	defer pods.mutex.Unlock()

	key := pod.Namespace + "/" + pod.Name
	oldpod := pods.Pods[key]
	if oldpod != nil {
		if reflect.DeepEqual(oldpod, pod) {
			log.Printf("no change to pod definition %s/%s", pod.Namespace, pod.Name)
			return false
		}
	}
	pods.Pods[key] = pod
	return true
}

func (pods *Pods) Delete(namespace, name string) {
	pods.mutex.Lock()
	defer pods.mutex.Unlock()

	delete(pods.Pods, namespace+"/"+name)
}

func (pods *Pods) Networks() (*Networks, error) {
	pods.mutex.RLock()
	defer pods.mutex.RUnlock()

	networks := NewNetworks()
	errorList := make([]error, 0)
	for _, pod := range pods.Pods {
		// TODO robustness, should continue with other pods in case one pod fails
		// and collect all errors
		err := networks.Add(pod)
		if err != nil {
			errorList = append(errorList, err)
		}
	}
	err := errors.Join(errorList...)
	return networks, err
}

func (pods *Pods) Copy() *Pods {
	pods.mutex.RLock()
	defer pods.mutex.RUnlock()
	res := NewPods()
	for key, pod := range pods.Pods {
		res.Pods[key] = pod
	}
	return res
}
