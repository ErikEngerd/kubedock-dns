package dns

import (
	"fmt"
	"k8s.io/klog/v2"
	"net"
	"strings"
	"sync"
	"time"
	"wamblee.org/kubedock/dns/internal/model"

	"github.com/miekg/dns"
)

type DNSServer interface {
	Resolve(r *dns.Msg) *dns.Msg
}

type ExternalDNSServer struct {
	upstreamDNSServer string
}

func NewExternalDNSServer(upstreamDNSServer string) *ExternalDNSServer {
	return &ExternalDNSServer{
		upstreamDNSServer: upstreamDNSServer,
	}
}

func (dnsServer *ExternalDNSServer) Resolve(r *dns.Msg) *dns.Msg {
	c := new(dns.Client)
	resp, _, err := c.Exchange(r, dnsServer.upstreamDNSServer)
	if err != nil {
		klog.Errorf("Error forwarding to upstream: %v", err)
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeServerFailure)
		return m
	}
	return resp
}

type KubeDockDns struct {
	mutex             sync.RWMutex
	networks          *model.Networks
	upstreamDnsServer DNSServer
	port              string
	searchDomain      string

	// These domains will not be resolved in the upstream server as well
	// as domains without dots. When a record is not found, the server
	// will return a SERVFAIL response, causing the client to retry.
	internalDomains []string

	overrideSourceIP model.IPAddress
}

func NewKubeDockDns(upstreamDnsServer DNSServer, port string, searchDomains string,
	internalDomains []string) *KubeDockDns {
	server := KubeDockDns{
		mutex:             sync.RWMutex{},
		networks:          model.NewNetworks(),
		upstreamDnsServer: upstreamDnsServer,
		port:              port,
		// final search suffix is the empty string for the case when we get
		searchDomain:    searchDomains,
		internalDomains: internalDomains,
	}
	return &server
}

func (dnsServer *KubeDockDns) OverrideSourceIP(sourceIP model.IPAddress) {
	dnsServer.overrideSourceIP = sourceIP
}

func (dnsServer *KubeDockDns) SetNetworks(networks *model.Networks) {
	dnsServer.mutex.Lock()
	defer dnsServer.mutex.Unlock()

	dnsServer.networks = networks
}

func (dnsServer *KubeDockDns) Serve() {
	dns.HandleFunc(".", dnsServer.handleDNSRequest)
	server := &dns.Server{Addr: dnsServer.port, Net: "udp"}
	klog.Infof("Starting DNS server on %s\n", server.Addr)
	err := server.ListenAndServe()
	if err != nil {
		klog.Fatalf("Failed to start server: %s\n ", err.Error())
	}
	defer server.Shutdown()
}

func (dnsServer *KubeDockDns) isInternal(host string) bool {
	host, _ = strings.CutSuffix(host, ".")
	host, _ = strings.CutSuffix(host, "."+dnsServer.searchDomain)
	if !strings.Contains(host, ".") {
		return true
	}
	for _, domain := range dnsServer.internalDomains {
		if strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func (dnsServer *KubeDockDns) handleDNSRequest(w dns.ResponseWriter, r *dns.Msg) {
	sourceIp := dnsServer.overrideSourceIP
	if sourceIp == "" {
		sourceIp = model.IPAddress(w.RemoteAddr().String())
		sourceIp = model.IPAddress(strings.Split(string(sourceIp), ":")[0])
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	question := r.Question
	fallback := func() *dns.Msg {
		return dnsServer.upstreamDnsServer.Resolve(r)
	}

	// Simple retry mechanism to wait for some time until the pod is known.
	// This can occur if a pod does a name lookup so early after it has started
	// that the IP address is not yet known in the DNS server.
	tend := time.Now().Add(20 * time.Second)
	for time.Now().Before(tend) {
		answer, err := dnsServer.answerQuestionWithNetworkSnapshot(question, sourceIp, fallback)
		if err == nil {
			m.Answer = answer
			w.WriteMsg(m)
			return
		}
		time.Sleep(1 * time.Second)
		klog.V(2).Infof("Retrying lookup")
	}

	klog.V(3).Infof("dns: %s: %s -> %s", sourceIp, question[0].Name, "SERVFAIL")
	m.Rcode = dns.RcodeServerFailure
	w.WriteMsg(m)
}

func (dnsServer *KubeDockDns) answerQuestionWithNetworkSnapshot(question []dns.Question, sourceIp model.IPAddress, fallback func() *dns.Msg) ([]dns.RR, error) {
	// limit the time we take the read lock by getting a snapshot of the network config
	// and using that. This allows the read locks to be short so that udpates to the network
	// config can be quick and do not depend on the time for submitting requests to an upstream
	// DNS
	dnsServer.mutex.RLock()
	networkSnapshot := dnsServer.networks
	dnsServer.mutex.RUnlock()
	answer, err := dnsServer.answerQuestion(question, networkSnapshot, sourceIp, fallback)
	return answer, err
}

func (dnsServer *KubeDockDns) answerQuestion(questions []dns.Question, networkSnapshot *model.Networks, sourceIp model.IPAddress,
	fallback func() *dns.Msg) ([]dns.RR, error) {
	answer := make([]dns.RR, 0)

	for _, question := range questions {
		var rrs []dns.RR
		internal := false
		if question.Qtype == dns.TypeA {
			internal = dnsServer.isInternal(question.Name)
			klog.V(2).Infof("dns: %s: A %s internal %v", sourceIp, question.Name, internal)
			rrs = resolveHostname(networkSnapshot, question, sourceIp, dnsServer.searchDomain)
		} else if question.Qtype == dns.TypePTR {
			klog.V(2).Infof("dns: %s: PTR %s", sourceIp, question.Name)
			rrs = resolveIP(networkSnapshot, question, sourceIp)
		}
		if len(rrs) > 0 {
			for _, rr := range rrs {
				answer = append(answer, rr)
			}
			continue
		}
		// when one question cannot be answered we delegate fully to the upstream server.
		if internal {
			return nil, fmt.Errorf("Internal hostname not (yet) found")
		} else {
			upstreamResponse := fallback()
			answer = append(answer, upstreamResponse.Answer...)
		}
	}
	return answer, nil
}

func resolveHostname(networks *model.Networks, question dns.Question, sourceIp model.IPAddress,
	searchDomain string) []dns.RR {
	klog.V(3).Infof("dns: %s: A %s", sourceIp, question.Name)

	hostname := question.Name[:len(question.Name)-1]
	if strings.HasSuffix(hostname, "."+searchDomain) {
		hostname = hostname[:len(hostname)-len(searchDomain)-1]
	}
	ips := networks.Lookup(sourceIp, model.Hostname(hostname))

	rrs := make([]dns.RR, 0)
	for _, ip := range ips {
		klog.V(3).Infof("dns: %s: %s -> %s", sourceIp, question.Name, ip)
		rr := createAResponse(question.Name, ip)
		rrs = append(rrs, rr)
	}
	return rrs
}

func PTRtoIP(ptr string) string {
	// Remove the .in-addr.arpa. suffix if present
	ptr = strings.TrimSuffix(ptr, ".in-addr.arpa.")

	// Split into octets
	parts := strings.Split(ptr, ".")

	// Reverse the octets
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}

	// Join back together
	return strings.Join(parts, ".")
}

func resolveIP(networks *model.Networks, question dns.Question, sourceIp model.IPAddress) []dns.RR {
	klog.V(3).Infof("dns: %s: A %s", sourceIp, question.Name)

	ip := PTRtoIP(question.Name)
	hosts := networks.ReverseLookup(
		sourceIp,
		model.IPAddress(ip))

	var rrs []dns.RR

	for _, host := range hosts {
		klog.V(3).Infof("dns: %s: %s -> %s", sourceIp, question.Name, host)
		rr := createPTRResponse(question.Name, host)
		rrs = append(rrs, rr)
	}
	return rrs
}

func createAResponse(questionName string, ip model.IPAddress) *dns.A {
	rr := &dns.A{
		Hdr: dns.RR_Header{
			Name:   questionName,
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: net.ParseIP(string(ip)),
	}
	return rr
}

func createPTRResponse(questionName string, host model.Hostname) dns.RR {
	klog.V(3).Infof("Creating ptr with %v", host)
	rr := &dns.PTR{
		Hdr: dns.RR_Header{
			Name:   questionName,
			Rrtype: dns.TypePTR,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Ptr: string(host) + ".",
	}
	return rr
}
