package dns

import (
	"github.com/miekg/dns"
	"github.com/stretchr/testify/suite"
	"k8s.io/klog/v2"
	"testing"
	"wamblee.org/kubedock/dns/internal/model"
)

type DNSTestSuite struct {
	suite.Suite

	pods *model.Pods
}

func (s *DNSTestSuite) SetupTest() {
	s.pods = model.NewPods()
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

func (s *DNSTestSuite) newPod(ip model.IPAddress, namespace string, name string, hostAliases []model.Hostname,
	networks []model.NetworkId) *model.Pod {
	pod, err := model.NewPod(ip, namespace, name, hostAliases, networks, true)
	s.Nil(err)
	s.NotNil(pod)
	return pod
}

func (s *DNSTestSuite) Test_LookupLocal() {
	pods := model.NewPods()
	pods.AddOrUpdate(s.newPod(
		"10.0.0.10", "kubedock", "pod-a", []model.Hostname{"db"},
		[]model.NetworkId{"test"},
	))
	pods.AddOrUpdate(s.newPod(
		"10.0.0.12", "kubedock", "pod-b", []model.Hostname{"service"},
		[]model.NetworkId{"test"},
	))
	networks, err := pods.Networks()
	s.Nil(err)

	upstream := DnsFunc(func(r *dns.Msg) *dns.Msg {
		s.Fail("Upstream DNS should not be called")
		return nil
	})

	dnsServer := NewKubeDockDns(upstream, ":1053", "xyz.svc.cluster.local", []string{})
	dnsServer.networks = networks

	// IP lookups
	s.verifyLookup("db.", "10.0.0.10", "10.0.0.10", dnsServer, networks)
	s.verifyLookup("db.xyz.svc.cluster.local.", "10.0.0.10", "10.0.0.10", dnsServer, networks)
	s.verifyLookup("db.", "10.0.0.12", "10.0.0.10", dnsServer, networks)
	s.verifyLookup("db.xyz.svc.cluster.local.", "10.0.0.12", "10.0.0.10", dnsServer, networks)

	// internal hostnames are never tried externally
	s.verifyLookup("db.", "10.0.0.11", "", dnsServer, networks)
	s.verifyLookup("db.xyz.svc.cluster.local.", "10.0.0.11", "", dnsServer, networks)

	// PTR lookups

	s.verifyReverseLookup("10.0.0.10", "10.0.0.10", "db.", dnsServer, networks)
	s.verifyReverseLookup("10.0.0.11", "10.0.0.10", "fallback.", dnsServer, networks)
	s.verifyReverseLookup("10.0.0.10", "10.0.0.12", "db.", dnsServer, networks)
	s.verifyReverseLookup("10.0.0.11", "10.0.0.12", "fallback.", dnsServer, networks)

}

func (s *DNSTestSuite) verifyLookup(hostname string, sourceIp string, expectedIp string, dnsServer *KubeDockDns, networks *model.Networks) {
	questions := []dns.Question{
		{
			Name:  hostname,
			Qtype: dns.TypeA,
		},
	}
	fallback := func() *dns.Msg {
		var rr dns.RR = createAResponse("dummyquestion", model.IPAddress("100.101.102.103"))
		m := &dns.Msg{
			Answer: []dns.RR{rr},
		}
		return m
	}
	rrs, err := dnsServer.answerQuestion(questions, networks, model.IPAddress(sourceIp), fallback)
	if expectedIp == "" {
		s.Require().NotNil(err)
		return
	}
	s.Require().Nil(err)
	klog.V(3).Infof("RRS %+v", rrs)
	s.Equal(1, len(rrs))
	s.Equal(expectedIp, rrs[0].(*dns.A).A.String())
}

func (s *DNSTestSuite) verifyReverseLookup(ip string, sourceIp string, expectedHost string, dnsServer *KubeDockDns, networks *model.Networks) {
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
	rrs, err := dnsServer.answerQuestion(questions, networks, model.IPAddress(sourceIp), fallback)
	s.Require().Nil(err)
	klog.V(3).Infof("RRS %+v", rrs)
	s.Equal(1, len(rrs))
	s.Equal(expectedHost, rrs[0].(*dns.PTR).Ptr)
}
