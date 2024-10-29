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
	nhostaliases := 0
	for _, pod := range network.IPToPod {
		s.True(slices.Contains(pod.Networks, network.Id))
		for _, hostalias := range pod.HostAliases {
			nhostaliases++
			pod2 := network.HostAliasToPod[hostalias]
			s.True(pod == pod2) // pointer equality
		}
	}
	s.Equal(nhostaliases, len(network.HostAliasToPod))
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

func (s *NetworkTestSuite) createPod(ip string, hostAliases []string, networks []string) *Pod {
	return &Pod{
		IP:        IPAddress(ip),
		Namespace: "kubedock",
		Name:      "host" + ip,
		HostAliases: support.MapSlice(hostAliases, func(x string) Hostname {
			return Hostname(x)
		}),
		Networks: support.MapSlice(networks, func(x string) NetworkId {
			return NetworkId(x)
		}),
	}
}

func (s *NetworkTestSuite) Test_EmptyNetwork() {
	networks, err := s.pods.Networks()
	s.Nil(err)
	s.checkNetworks(networks)
}

func (s *NetworkTestSuite) assertIps(expected []IPAddress,
	actual []IPAddress) {

	expected2 := slices.Clone(expected)
	actual2 := slices.Clone(actual)

	slices.Sort(expected2)
	slices.Sort(actual2)

	s.Equal(expected, actual2)
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
		pod := s.createPod(podInfo.ip, podInfo.hosts, podInfo.networks)
		updated := s.pods.AddOrUpdate(pod)
		s.Equal(podInfo.updated, updated)
	}
	networks, err := s.pods.Networks()
	if networkTest.errorsExpected {
		s.NotNil(err)
	} else {
		s.Nil(err)
	}
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

}

func (s *NetworkTestSuite) Test_ThreePodsWhereSecondIsInvalid() {

}

func (s *NetworkTestSuite) Test_MultipleNetworksInOnePod() {

}
