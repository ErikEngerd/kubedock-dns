package main

import (
	"github.com/stretchr/testify/suite"
	"slices"
	"testing"
	"wamblee.org/kubedock/dns/internal/support"
)

type NetworkTestSuite struct {
	suite.Suite

	pods *Pods
}

func (s *NetworkTestSuite) SetupTest() {
	s.pods = NewPods()
}

func (s *NetworkTestSuite) TearDownTest() {
}

func TestNetorkTestSuite(t *testing.T) {
	suite.Run(t, &NetworkTestSuite{})
}

func (s *NetworkTestSuite) checkNetwork(network *Network) {
	s.Greater(len(network.IPToPod), 0)
	s.Greater(len(network.HostAliasToPod), 0)
	hostaliases := make(map[Hostname]bool)
	for _, pod := range network.IPToPod {
		s.True(slices.Contains(pod.Networks, network.Id))
		for _, hostalias := range pod.HostAliases {
			hostaliases[hostalias] = true
			pod2 := network.HostAliasToPod[hostalias]
			s.True(pod == pod2) // pointer equality
		}
	}
	s.Equal(len(hostaliases), len(network.HostAliasToPod))
}

func (s *NetworkTestSuite) checkNetworks(networks *Networks) {

	networkNames := make(map[NetworkId]*Network)

	for ip, networkMap := range networks.IpToNetworks {

		// * for every IP, the networks in the value must contain the IP
		for networkId, network := range networkMap {
			networkNames[networkId] = network
			s.Equal(networkId, network.Id)
			// the IP must be in the network
			s.NotNil(network.IPToPod[ip])
			// Every IP contained in the network must be in IP to Network map
			// and point to the same network
			for ip, _ := range network.IPToPod {
				networkmap2 := networks.IpToNetworks[ip]
				s.NotNil(networkmap2)
				if networkmap2 != nil {
					s.True(networkmap2[networkId] == network)
				}
			}
			// The network must be in the name map
			s.NotNil(networks.NameToNetwork[networkId])
			s.True(network == networks.NameToNetwork[networkId])
		}
	}

	s.Equal(len(networkNames), len(networks.NameToNetwork))

	// check the individual networks
	for _, network := range networkNames {
		s.checkNetwork(network)
	}
}

func (s *NetworkTestSuite) createPod(ip string, hostAliases []string, networks []string) (*Pod, error) {
	pod, err := NewPod(
		IPAddress(ip),
		"kubedock",
		"host"+ip,
		support.MapSlice(hostAliases, func(x string) Hostname {
			return Hostname(x)
		}),
		support.MapSlice(networks, func(x string) NetworkId {
			return NetworkId(x)
		}),
	)
	return pod, err
}

func (s *NetworkTestSuite) Test_EmptyNetwork() {
	networks, err := s.pods.Networks()
	s.Nil(err)
	s.checkNetworks(networks)
}

type PodInfo struct {
	ip       string
	hosts    []string
	networks []string
	updated  bool
}
type Lookup struct {
	sourceIp string
	host     string
	ips      []string
}
type ReverseLookup struct {
	sourceIp string
	ip       string
	hosts    []string
}
type NetworkTest struct {
	pods           []PodInfo
	errorsExpected bool
	lookups        []Lookup
	reverseLookups []ReverseLookup
}

func (s *NetworkTestSuite) runTest(networkTest *NetworkTest) {
	for _, podInfo := range networkTest.pods {
		pod, err := s.createPod(podInfo.ip, podInfo.hosts, podInfo.networks)
		s.Require().Nil(err)
		updated := s.pods.AddOrUpdate(pod)
		s.Equal(podInfo.updated, updated)
	}
	networks, err := s.pods.Networks()
	if networkTest.errorsExpected {
		s.NotNil(err)
	} else {
		s.Nil(err)
	}
	networks.Log()
	s.checkNetworks(networks)
	for _, lookup := range networkTest.lookups {
		ips := networks.Lookup(IPAddress(lookup.sourceIp),
			Hostname(lookup.host))
		ipsString := support.MapSlice(ips, func(x IPAddress) string {
			return string(x)
		})
		slices.Sort(ipsString)
		expectedIps := slices.Clone(lookup.ips)
		slices.Sort(expectedIps)
		s.Equal(expectedIps, ipsString)
	}
	for _, reverseLookup := range networkTest.reverseLookups {
		hosts := networks.ReverseLookup(IPAddress(reverseLookup.sourceIp),
			IPAddress(reverseLookup.ip))
		hostsString := support.MapSlice(hosts, func(x Hostname) string {
			return string(x)
		})
		slices.Sort(hostsString)
		expectedHosts := slices.Clone(reverseLookup.hosts)
		slices.Sort(expectedHosts)
		s.Equal(expectedHosts, hostsString)
	}
}

func (s *NetworkTestSuite) Test_InvalidHostname() {
	pod, err := s.createPod("a", []string{"-db"}, []string{"test"})
	s.Nil(pod)
	s.NotNil(err)
}

func (s *NetworkTestSuite) Test_Pods() {
	pod1, err := s.createPod("a", []string{"db"}, []string{"test"})
	s.Nil(err)
	pod2, err := s.createPod("b", []string{"server"}, []string{"test"})
	s.Nil(err)

	s.True(s.pods.AddOrUpdate(pod1))
	s.False(s.pods.AddOrUpdate(pod1))
	pod1a, ok := s.pods.Pods.Get(pod1.Namespace + "/" + pod1.Name)
	s.True(ok)
	s.Equal(pod1, pod1a)
	s.Equal(1, s.pods.Pods.Len())

	s.True(s.pods.AddOrUpdate(pod2))
	s.False(s.pods.AddOrUpdate(pod2))
	pod2.HostAliases = []Hostname{Hostname("abc")}
	s.True(s.pods.AddOrUpdate(pod2))
	pod2a, ok := s.pods.Pods.Get(pod2.Namespace + "/" + pod2.Name)
	s.True(ok)
	s.Equal(pod2, pod2a)
	s.Equal(2, s.pods.Pods.Len())

	s.pods.Delete(pod1.Namespace, pod1.Name)
	s.Equal(1, s.pods.Pods.Len())
	pod2b, _ := s.pods.Pods.Get(pod2.Namespace + "/" + pod2.Name)
	s.Equal(pod2, pod2b)

	s.pods.Delete(pod2.Namespace, pod2.Name)
	s.Equal(0, s.pods.Pods.Len())

}

func (s *NetworkTestSuite) Test_SinglePod() {
	test := NetworkTest{
		pods: []PodInfo{
			{
				ip:       "a",
				hosts:    []string{"db"},
				networks: []string{"test1"},
				updated:  true,
			},
		},
		errorsExpected: false,
		lookups: []Lookup{
			{
				sourceIp: "a",
				host:     "db",
				ips:      []string{"a"},
			},
			{
				sourceIp: "unknownip",
				host:     "db",
				ips:      []string{},
			},
		},
		reverseLookups: []ReverseLookup{
			{
				sourceIp: "a",
				ip:       "a",
				hosts:    []string{"db"},
			},
		},
	}
	s.runTest(&test)
}

func (s *NetworkTestSuite) Test_SinglePodWithUnknownIP() {
	test := NetworkTest{
		pods: []PodInfo{
			{
				ip:       UNKNOWN_IP_PREFIX + "a",
				hosts:    []string{"db"},
				networks: []string{"test1"},
				updated:  true,
			},
			{
				ip:       "b",
				hosts:    []string{"svc"},
				networks: []string{"test1"},
				updated:  true,
			},
		},
		errorsExpected: false,
		lookups: []Lookup{
			{
				sourceIp: UNKNOWN_IP_PREFIX + "a",
				host:     "db",
				ips:      []string{},
			},
		},
		reverseLookups: []ReverseLookup{
			{
				sourceIp: UNKNOWN_IP_PREFIX + "a",
				ip:       UNKNOWN_IP_PREFIX + "a",
				hosts:    []string{},
			},
			{
				sourceIp: "b",
				ip:       UNKNOWN_IP_PREFIX + "a",
				hosts:    []string{},
			},
		},
	}
	s.runTest(&test)
}

func (s *NetworkTestSuite) Test_TwoPodsSameNetwork() {
	test := NetworkTest{
		pods: []PodInfo{
			{
				ip:       "a",
				hosts:    []string{"db"},
				networks: []string{"test1"},
				updated:  true,
			},
			{
				ip:       "b",
				hosts:    []string{"server"},
				networks: []string{"test1"},
				updated:  true,
			},
		},
		errorsExpected: false,
		lookups: []Lookup{
			{
				sourceIp: "a",
				host:     "db",
				ips:      []string{"a"},
			},
			{
				sourceIp: "a",
				host:     "server",
				ips:      []string{"b"},
			},
			{
				sourceIp: "b",
				host:     "db",
				ips:      []string{"a"},
			},
			{
				sourceIp: "b",
				host:     "server",
				ips:      []string{"b"},
			},
			{
				sourceIp: "unknownip",
				host:     "server",
				ips:      []string{},
			},
			{
				sourceIp: "unknownip",
				host:     "db",
				ips:      []string{},
			},
		},
		reverseLookups: []ReverseLookup{
			{
				sourceIp: "a",
				ip:       "a",
				hosts:    []string{"db"},
			},
			{
				sourceIp: "a",
				ip:       "b",
				hosts:    []string{"server"},
			},
			{
				sourceIp: "b",
				ip:       "a",
				hosts:    []string{"db"},
			},
			{
				sourceIp: "b",
				ip:       "b",
				hosts:    []string{"server"},
			},
		},
	}
	s.runTest(&test)
}

func (s *NetworkTestSuite) Test_TwoPodsDifferentNetwork() {
	test := NetworkTest{
		pods: []PodInfo{
			{
				ip:       "a",
				hosts:    []string{"db"},
				networks: []string{"test1"},
				updated:  true,
			},
			{
				ip:       "b",
				hosts:    []string{"server"},
				networks: []string{"test2"},
				updated:  true,
			},
		},
		errorsExpected: false,
		lookups: []Lookup{
			{
				sourceIp: "a",
				host:     "db",
				ips:      []string{"a"},
			},
			{
				sourceIp: "a",
				host:     "server",
				ips:      []string{},
			},
			{
				sourceIp: "b",
				host:     "db",
				ips:      []string{},
			},
			{
				sourceIp: "b",
				host:     "server",
				ips:      []string{"b"},
			},
			{
				sourceIp: "unknownip",
				host:     "server",
				ips:      []string{},
			},
			{
				sourceIp: "unknownip",
				host:     "db",
				ips:      []string{},
			},
		},
		reverseLookups: []ReverseLookup{
			{
				sourceIp: "a",
				ip:       "a",
				hosts:    []string{"db"},
			},
			{
				sourceIp: "a",
				ip:       "b",
				hosts:    []string{},
			},
			{
				sourceIp: "b",
				ip:       "a",
				hosts:    []string{},
			},
			{
				sourceIp: "b",
				ip:       "b",
				hosts:    []string{"server"},
			},
		},
	}
	s.runTest(&test)
}

func (s *NetworkTestSuite) Test_ThreePodsWhereSecondIsInvalid() {
	test := NetworkTest{
		pods: []PodInfo{
			{
				ip:       "a",
				hosts:    []string{"db"},
				networks: []string{"test1"},
				updated:  true,
			},
			// duplicate host name in second pod
			{
				ip:       "a2",
				hosts:    []string{"db"},
				networks: []string{"test1"},
				updated:  true,
			},
			{
				ip:       "b",
				hosts:    []string{"server"},
				networks: []string{"test1"},
				updated:  true,
			},
		},
		errorsExpected: true,
		lookups: []Lookup{
			{
				sourceIp: "a",
				host:     "db",
				ips:      []string{"a"},
			},
			{
				sourceIp: "a",
				host:     "server",
				ips:      []string{"b"},
			},
			{
				sourceIp: "b",
				host:     "db",
				ips:      []string{"a"},
			},
			{
				sourceIp: "b",
				host:     "server",
				ips:      []string{"b"},
			},
			{
				sourceIp: "unknownip",
				host:     "server",
				ips:      []string{},
			},
			{
				sourceIp: "unknownip",
				host:     "db",
				ips:      []string{},
			},
		},
		reverseLookups: []ReverseLookup{
			{
				sourceIp: "a",
				ip:       "a",
				hosts:    []string{"db"},
			},
			{
				sourceIp: "a",
				ip:       "b",
				hosts:    []string{"server"},
			},
			{
				sourceIp: "b",
				ip:       "a",
				hosts:    []string{"db"},
			},
			{
				sourceIp: "b",
				ip:       "b",
				hosts:    []string{"server"},
			},
		},
	}
	s.runTest(&test)
}

// the multiple tests with same container host names scenario
func (s *NetworkTestSuite) Test_MultipleNetworksAreSeparate() {
	test := NetworkTest{
		pods: []PodInfo{
			{
				ip:       "a",
				hosts:    []string{"db"},
				networks: []string{"test1"},
				updated:  true,
			},
			{
				ip:       "b",
				hosts:    []string{"server"},
				networks: []string{"test1"},
				updated:  true,
			},
			{
				ip:       "a2",
				hosts:    []string{"db"},
				networks: []string{"test2"},
				updated:  true,
			},
			{
				ip:       "b2",
				hosts:    []string{"server"},
				networks: []string{"test2"},
				updated:  true,
			},
		},
		errorsExpected: false,
		lookups: []Lookup{
			{
				sourceIp: "a",
				host:     "db",
				ips:      []string{"a"},
			},
			{
				sourceIp: "a2",
				host:     "db",
				ips:      []string{"a2"},
			},
			{
				sourceIp: "a",
				host:     "server",
				ips:      []string{"b"},
			},
			{
				sourceIp: "a2",
				host:     "server",
				ips:      []string{"b2"},
			},
			{
				sourceIp: "b",
				host:     "db",
				ips:      []string{"a"},
			},
			{
				sourceIp: "b2",
				host:     "db",
				ips:      []string{"a2"},
			},
			{
				sourceIp: "b",
				host:     "server",
				ips:      []string{"b"},
			},
			{
				sourceIp: "b2",
				host:     "server",
				ips:      []string{"b2"},
			},
			{
				sourceIp: "unknownip",
				host:     "server",
				ips:      []string{},
			},
			{
				sourceIp: "unknownip",
				host:     "db",
				ips:      []string{},
			},
		},
		reverseLookups: []ReverseLookup{
			{
				sourceIp: "a",
				ip:       "a",
				hosts:    []string{"db"},
			},
			{
				sourceIp: "a2",
				ip:       "a",
				hosts:    []string{},
			},
			{
				sourceIp: "a2",
				ip:       "a2",
				hosts:    []string{"db"},
			},
			{
				sourceIp: "a",
				ip:       "b",
				hosts:    []string{"server"},
			},
			{
				sourceIp: "a",
				ip:       "b2",
				hosts:    []string{},
			},
			{
				sourceIp: "a2",
				ip:       "b2",
				hosts:    []string{"server"},
			},
			{
				sourceIp: "b",
				ip:       "a",
				hosts:    []string{"db"},
			},
			{
				sourceIp: "b",
				ip:       "b",
				hosts:    []string{"server"},
			},
		},
	}
	s.runTest(&test)
}

// specialist scenario probably not encountered with testcontainers, but
// still generally supported.
func (s *NetworkTestSuite) Test_MultipleNetworksInOnePod() {
	test := NetworkTest{
		pods: []PodInfo{
			{
				ip:       "db1",
				hosts:    []string{"db1", "db"},
				networks: []string{"test1"},
				updated:  true,
			},
			{
				ip:       "db2",
				hosts:    []string{"db2", "db"},
				networks: []string{"test2"},
				updated:  true,
			},
			// server can access services in test1 and test2 network
			{
				ip:       "server",
				hosts:    []string{"server"},
				networks: []string{"test1", "test2"},
				updated:  true,
			},
		},
		errorsExpected: false,
		lookups: []Lookup{
			{
				sourceIp: "server",
				host:     "db1",
				ips:      []string{"db1"},
			},
			{
				sourceIp: "server",
				host:     "db2",
				ips:      []string{"db2"},
			},
			{
				sourceIp: "server",
				host:     "db",
				ips:      []string{"db1", "db2"},
			},
		},
		reverseLookups: []ReverseLookup{
			{
				sourceIp: "server",
				ip:       "db1",
				hosts:    []string{"db1", "db"},
			},
			{
				sourceIp: "server",
				ip:       "db2",
				hosts:    []string{"db2", "db"},
			},
		},
	}
	s.runTest(&test)
}

func (s *NetworkTestSuite) Test_DuplicateHostsInPod() {
	test := NetworkTest{
		pods: []PodInfo{
			{
				ip:       "a",
				hosts:    []string{"db1", "db1"},
				networks: []string{"test1"},
				updated:  true,
			},
		},
		errorsExpected: false,
		lookups: []Lookup{
			{
				sourceIp: "a",
				host:     "db1",
				ips:      []string{"a"},
			},
		},
		reverseLookups: []ReverseLookup{
			{
				sourceIp: "a",
				ip:       "a",
				hosts:    []string{"db1"},
			},
		},
	}
	s.runTest(&test)
}

func (s *NetworkTestSuite) Test_DuplicateNetworksInPod() {
	test := NetworkTest{
		pods: []PodInfo{
			{
				ip:       "a",
				hosts:    []string{"db1"},
				networks: []string{"test1", "test1"},
				updated:  true,
			},
		},
		errorsExpected: false,
		lookups: []Lookup{
			{
				sourceIp: "a",
				host:     "db1",
				ips:      []string{"a"},
			},
		},
		reverseLookups: []ReverseLookup{
			{
				sourceIp: "a",
				ip:       "a",
				hosts:    []string{"db1"},
			},
		},
	}
	s.runTest(&test)
}
