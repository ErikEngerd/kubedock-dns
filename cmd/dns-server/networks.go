package main

import (
	"fmt"
	"log"
	"reflect"
	"slices"
	"sync"
	"wamblee.org/kubedock/dns/internal/support"
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

func (pod *Pod) Copy() *Pod {
	return &Pod{
		IP:          pod.IP,
		Namespace:   pod.Namespace,
		Name:        pod.Name,
		HostAliases: slices.Clone(pod.HostAliases),
		Networks:    slices.Clone(pod.Networks),
	}
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
			return fmt.Errorf("Pod %s/%s: hostAlias %s in network %s already mapped to %s/%s",
				pod.Namespace, pod.Name, hostAlias, net.Id, existingPod.Namespace, existingPod.Name)
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

type PodError struct {
	Pod *Pod
	Err error
}

func (err *PodError) Error() string {
	return fmt.Sprintf("[%s/%s]: %v",
		err.Pod.Namespace, err.Pod.Name, err.Err)
}

func NewPodError(pod *Pod, err error) *PodError {
	return &PodError{
		Pod: pod,
		Err: err,
	}
}

func (net *Networks) Add(pod *Pod) *PodError {
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
		err := network.Add(pod)
		if err != nil {
			return NewPodError(pod, err)
		}

		if net.IpToNetworks[pod.IP] == nil {
			net.IpToNetworks[pod.IP] = make(NetworkMap)
		}
		net.IpToNetworks[pod.IP][networkId] = network
		net.NameToNetwork[networkId] = network
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
}

func (net *Networks) Lookup(sourceIp IPAddress, hostname Hostname) []IPAddress {
	log.Printf("Lookup source ip '%s' host '%s'", sourceIp, hostname)
	networks := net.IpToNetworks[sourceIp]
	if networks == nil {
		return make([]IPAddress, 0)
	}
	res := make([]IPAddress, 0)
	for _, network := range networks {
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
	// Using a linked map to preserver insertion order so we get more deterministic
	// behavior in when building the network in the case of misconfiguration.
	Pods *support.LinkedMap[string, *Pod]
}

func NewPods() *Pods {
	return &Pods{
		mutex: sync.RWMutex{},
		Pods:  support.NewLinkedMap[string, *Pod](),
	}
}

func (pods *Pods) AddOrUpdate(pod *Pod) bool {
	pods.mutex.Lock()
	defer pods.mutex.Unlock()

	key := pod.Namespace + "/" + pod.Name
	oldpod, _ := pods.Pods.Get(key)
	if oldpod != nil {
		if reflect.DeepEqual(oldpod, pod) {
			log.Printf("no change to pod definition %s/%s", pod.Namespace, pod.Name)
			return false
		}
	}
	log.Printf("Processing pod networking config %s/%s", pod.Namespace, pod.Name)
	pods.Pods.Put(key, pod.Copy())
	return true
}

func (pods *Pods) Delete(namespace, name string) {
	pods.mutex.Lock()
	defer pods.mutex.Unlock()

	pods.Pods.Delete(namespace + "/" + name)
}

type PodErrors struct {
	Errors []*PodError
}

func NewPodErrors(errors []*PodError) *PodErrors {
	if len(errors) == 0 {
		return nil
	}
	return &PodErrors{
		Errors: errors,
	}
}

func (e *PodErrors) Error() string {
	res := ""
	for _, err := range e.Errors {
		res = res + err.Error() + "\n"
	}
	return res
}

func (e *PodErrors) FirstError(pod *Pod) error {
	for _, err := range e.Errors {
		if err.Pod.Namespace == pod.Namespace && err.Pod.Name == pod.Name {
			return err
		}
	}
	return nil
}

func (pods *Pods) Networks() (*Networks, *PodErrors) {
	pods.mutex.RLock()
	defer pods.mutex.RUnlock()

	networks := NewNetworks()
	errorList := make([]*PodError, 0)
	for _, pod := range pods.Pods.Iter() {
		err := networks.Add(pod)
		if err != nil {
			errorList = append(errorList, err)
		}
	}
	err := NewPodErrors(errorList)
	return networks, err
}

func (pods *Pods) Copy() *Pods {
	pods.mutex.RLock()
	defer pods.mutex.RUnlock()
	res := NewPods()
	for key, value := range pods.Pods.Iter() {
		res.Pods.Put(key, value)
	}
	return res
}
