package main

import (
	"log"
	"net"
	"strings"
	"sync"

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
		log.Printf("Error forwarding to upstream: %v", err)
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeServerFailure)
		return m
	}
	return resp
}

type KubeDockDns struct {
	mutex             sync.RWMutex
	networks          *Networks
	upstreamDnsServer DNSServer
	port              string
	searchDomain      string

	overrideSourceIP IPAddress
}

func NewKubeDockDns(upstreamDnsServer DNSServer, port string, searchDomains string) *KubeDockDns {
	server := KubeDockDns{
		mutex:             sync.RWMutex{},
		networks:          NewNetworks(),
		upstreamDnsServer: upstreamDnsServer,
		port:              port,
		// final search suffix is the empty string for the case when we get
		searchDomain: searchDomains,
	}
	return &server
}

func (dnsServer *KubeDockDns) OverrideSourceIP(sourceIP IPAddress) {
	dnsServer.overrideSourceIP = sourceIP
}

func (dnsServer *KubeDockDns) SetNetworks(networks *Networks) {
	dnsServer.mutex.Lock()
	defer dnsServer.mutex.Unlock()

	dnsServer.networks = networks
}

func (dnsServer *KubeDockDns) Serve() {
	dns.HandleFunc(".", dnsServer.handleDNSRequest)
	server := &dns.Server{Addr: dnsServer.port, Net: "udp"}
	log.Printf("Starting DNS server on %s\n", server.Addr)
	err := server.ListenAndServe()
	defer server.Shutdown()
	if err != nil {
		log.Fatalf("Failed to start server: %s\n ", err.Error())
	}
}

func (dnsServer *KubeDockDns) handleDNSRequest(w dns.ResponseWriter, r *dns.Msg) {
	// limit the time we take the read lock by getting a snapshot of the network config
	// and using that. This allows the read locks to be short so that udpates to the network
	// config can be quick and do not depend on the time for submitting requests to an upstream
	// DNS
	dnsServer.mutex.RLock()
	networkSnapshot := dnsServer.networks
	dnsServer.mutex.RUnlock()

	sourceIp := dnsServer.overrideSourceIP
	if sourceIp == "" {
		sourceIp = IPAddress(w.RemoteAddr().String())
		sourceIp = IPAddress(strings.Split(string(sourceIp), ":")[0])
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	question := r.Question
	fallback := func() *dns.Msg {
		return dnsServer.upstreamDnsServer.Resolve(r)
	}
	answer := dnsServer.answerQuestion(question, networkSnapshot, sourceIp, fallback)

	m.Answer = answer
	w.WriteMsg(m)
}

func (dnsServer *KubeDockDns) answerQuestion(questions []dns.Question, networkSnapshot *Networks, sourceIp IPAddress,
	fallback func() *dns.Msg) []dns.RR {
	answer := make([]dns.RR, 0)

	for _, question := range questions {
		var rrs []dns.RR
		if question.Qtype == dns.TypeA {
			log.Printf("dns: A %s", question.Name)
			rrs = resolveHostname(networkSnapshot, question, sourceIp, dnsServer.searchDomain)
		} else if question.Qtype == dns.TypePTR {
			log.Printf("dns: PTR %s", question.Name)
			rrs = resolveIP(networkSnapshot, question, sourceIp)
		}
		if len(rrs) > 0 {
			for _, rr := range rrs {
				answer = append(answer, rr)
			}
			continue
		}
		// when one question cannot be answered we delegate fully to the upstream server.

		upstreamResponse := fallback()
		return upstreamResponse.Answer
	}
	return answer
}

func resolveHostname(networks *Networks, question dns.Question, sourceIp IPAddress,
	searchDomain string) []dns.RR {
	log.Printf("dns: A %s", question.Name)

	hostname := question.Name[:len(question.Name)-1]
	if strings.HasSuffix(hostname, "."+searchDomain) {
		hostname = hostname[:len(hostname)-len(searchDomain)-1]
	}
	ips := networks.Lookup(sourceIp, Hostname(hostname))

	rrs := make([]dns.RR, 0)
	for _, ip := range ips {
		log.Printf("dns: %s -> %s", question.Name, ip)
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

func resolveIP(networks *Networks, question dns.Question, sourceIp IPAddress) []dns.RR {
	log.Printf("dns: A %s", question.Name)

	ip := PTRtoIP(question.Name)
	hosts := networks.ReverseLookup(
		sourceIp,
		IPAddress(ip))

	var rrs []dns.RR

	for _, host := range hosts {
		log.Printf("dns: %s -> %s", question.Name, host)
		rr := createPTRResponse(question.Name, host)
		rrs = append(rrs, rr)
	}
	return rrs
}

func createAResponse(questionName string, ip IPAddress) *dns.A {
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

func createPTRResponse(questionName string, host Hostname) dns.RR {
	log.Printf("Creating ptr with %v", host)
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
