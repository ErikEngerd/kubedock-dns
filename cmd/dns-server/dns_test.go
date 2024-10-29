package main

import (
	"github.com/miekg/dns"
	"github.com/stretchr/testify/suite"
	"log"
	"net"
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

func (s *DNSTestSuite) Test_LookupLocal() {
	pods := NewPods()
	pod := Pod{
		IP:          "10.0.0.10",
		Namespace:   "kubedock",
		Name:        "pod-a",
		HostAliases: []Hostname{"db"},
		Networks:    []NetworkId{"test"},
	}
	pods.AddOrUpdate(&pod)
	networks, err := pods.Networks()
	s.Nil(err)

	upstream := DnsFunc(func(r *dns.Msg) *dns.Msg {
		s.Fail("Upstream DNS should not be called")
		return nil
	})
	fallback := func() *dns.Msg {
		s.Fail("Upstream DNS should not be called")
		return nil
	}
	dnsServer := NewKubeDockDns(upstream, ":1053", "xyz.svc.cluster.local")
	dnsServer.networks = networks

	questions := []dns.Question{
		{
			Name:  "db.",
			Qtype: dns.TypeA,
		},
	}
	rrs := dnsServer.answerQuestion(questions, networks, "10.0.0.10", fallback)
	log.Printf("RRS %+v", rrs)
	s.Equal(1, len(rrs))
	s.Equal(net.ParseIP("10.0.0.10"), rrs[0].(*dns.A).A)

	questions = []dns.Question{
		{
			Name:  "db.xyz.svc.cluster.local.",
			Qtype: dns.TypeA,
		},
	}
	rrs = dnsServer.answerQuestion(questions, networks, "10.0.0.10", fallback)
	log.Printf("RRS %+v", rrs)
	s.Equal(1, len(rrs))
	s.Equal(net.ParseIP("10.0.0.10"), rrs[0].(*dns.A).A)
}
