package main

import (
	"github.com/miekg/dns"
	"github.com/stretchr/testify/suite"
	"k8s.io/klog/v2"
	"testing"
)

type DNSTestSuite struct {
	suite.Suite

	pods *Pods
}

func (s *DNSTestSuite) SetupTest() {
	s.pods = NewPods()
}

func (s *DNSTestSuite) TearDownTest() {
}

func TestDNSTestSuite(t *testing.T) {
	suite.Run(t, &DNSTestSuite{})
}

type DnsFunc func(r *dns.Msg) *dns.Msg

func (dnsFunc DnsFunc) Resolve(r *dns.Msg) *dns.Msg {
	return dnsFunc(r)
}

func (s *DNSTestSuite) newPod(ip IPAddress, namespace string, name string, hostAliases []Hostname,
	networks []NetworkId) *Pod {
	pod, err := NewPod(ip, namespace, name, hostAliases, networks)
	s.Nil(err)
	s.NotNil(pod)
	return pod
}

func (s *DNSTestSuite) Test_LookupLocal() {
	pods := NewPods()
	pods.AddOrUpdate(s.newPod(
		"10.0.0.10", "kubedock", "pod-a", []Hostname{"db"},
		[]NetworkId{"test"},
	))
	pods.AddOrUpdate(s.newPod(
		"10.0.0.12", "kubedock", "pod-b", []Hostname{"service"},
		[]NetworkId{"test"},
	))
	networks, err := pods.Networks()
	s.Nil(err)

	upstream := DnsFunc(func(r *dns.Msg) *dns.Msg {
		s.Fail("Upstream DNS should not be called")
		return nil
	})

	dnsServer := NewKubeDockDns(upstream, ":1053", "xyz.svc.cluster.local")
	dnsServer.networks = networks

	// IP lookups
	s.verifyLookup("db.", "10.0.0.10", "10.0.0.10", dnsServer, networks)
	s.verifyLookup("db.xyz.svc.cluster.local.", "10.0.0.10", "10.0.0.10", dnsServer, networks)
	s.verifyLookup("db.", "10.0.0.12", "10.0.0.10", dnsServer, networks)
	s.verifyLookup("db.xyz.svc.cluster.local.", "10.0.0.12", "10.0.0.10", dnsServer, networks)

	s.verifyLookup("db.", "10.0.0.11", "100.101.102.103", dnsServer, networks)
	s.verifyLookup("db.xyz.svc.cluster.local.", "10.0.0.11", "100.101.102.103", dnsServer, networks)

	// PTR lookups

	s.verifyReverseLookup("10.0.0.10", "10.0.0.10", "db.", dnsServer, networks)
	s.verifyReverseLookup("10.0.0.11", "10.0.0.10", "fallback.", dnsServer, networks)
	s.verifyReverseLookup("10.0.0.10", "10.0.0.12", "db.", dnsServer, networks)
	s.verifyReverseLookup("10.0.0.11", "10.0.0.12", "fallback.", dnsServer, networks)

}

func (s *DNSTestSuite) verifyLookup(hostname string, sourceIp string, expectedIp string, dnsServer *KubeDockDns, networks *Networks) {
	questions := []dns.Question{
		{
			Name:  hostname,
			Qtype: dns.TypeA,
		},
	}
	fallback := func() *dns.Msg {
		var rr dns.RR = createAResponse("dummyquestion", IPAddress("100.101.102.103"))
		m := &dns.Msg{
			Answer: []dns.RR{rr},
		}
		return m
	}
	rrs := dnsServer.answerQuestion(questions, networks, IPAddress(sourceIp), fallback)
	klog.V(3).Infof("RRS %+v", rrs)
	s.Equal(1, len(rrs))
	s.Equal(expectedIp, rrs[0].(*dns.A).A.String())
}

func (s *DNSTestSuite) verifyReverseLookup(ip string, sourceIp string, expectedHost string, dnsServer *KubeDockDns, networks *Networks) {
	questions := []dns.Question{
		{
			Name:  ip,
			Qtype: dns.TypePTR,
		},
	}
	fallback := func() *dns.Msg {
		var rr dns.RR = createPTRResponse("dummyquestion", "fallback")
		m := &dns.Msg{
			Answer: []dns.RR{rr},
		}
		return m
	}
	rrs := dnsServer.answerQuestion(questions, networks, IPAddress(sourceIp), fallback)
	klog.V(3).Infof("RRS %+v", rrs)
	s.Equal(1, len(rrs))
	s.Equal(expectedHost, rrs[0].(*dns.PTR).Ptr)
}
