package main

import (
	"log"
	"net"
	"strings"
	"sync"

	"github.com/miekg/dns"
)

type KubeDockDns struct {
	mutex             sync.RWMutex
	networks          *Networks
	upstreamDnsServer string
	port              string

	overrideSourceIP IPAddress
}

func NewKubeDockDns(upstreamDnsServer string, port string) *KubeDockDns {
	server := KubeDockDns{
		mutex:             sync.RWMutex{},
		networks:          NewNetworks(),
		upstreamDnsServer: upstreamDnsServer,
		port:              port,
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

	for _, question := range r.Question {
		var rrs []dns.RR
		if question.Qtype == dns.TypeA {
			log.Printf("dns: A %s", question.Name)
			rrs = resolveHostname(networkSnapshot, question, sourceIp)
		} else if question.Qtype == dns.TypePTR {
			log.Printf("dns: PTR %s", question.Name)
			rrs = resolveIP(networkSnapshot, question, sourceIp)
		}
		if len(rrs) > 0 {
			for _, rr := range rrs {
				m.Answer = append(m.Answer, rr)
			}
			continue
		}
		log.Printf("dns: forwarding question %+v to upstream", question)
		upstreamResponse := dnsServer.forwardToUpstream(r)
		m.Answer = append(m.Answer, upstreamResponse.Answer...)
	}

	log.Printf("Writing response: %d", len(m.Answer))
	w.WriteMsg(m)
}

func resolveHostname(networks *Networks, question dns.Question, sourceIp IPAddress) []dns.RR {
	log.Printf("dns: A %s", question.Name)
	ips := networks.Lookup(
		sourceIp,
		Hostname(question.Name[:len(question.Name)-1]))

	rrs := make([]dns.RR, 0)
	for _, ip := range ips {
		log.Printf("dns: %s -> %s", question.Name, ip)
		rr := createAResponse(question, ip)
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
		rr := createPTRResponse(question, host)
		rrs = append(rrs, rr)
	}
	return rrs
}

func createAResponse(question dns.Question, ip IPAddress) *dns.A {
	rr := &dns.A{
		Hdr: dns.RR_Header{
			Name:   question.Name,
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: net.ParseIP(string(ip)),
	}
	return rr
}

func createPTRResponse(question dns.Question, host Hostname) dns.RR {
	log.Printf("Creating ptr with %v", host)
	rr := &dns.PTR{
		Hdr: dns.RR_Header{
			Name:   question.Name,
			Rrtype: dns.TypePTR,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Ptr: string(host) + ".",
	}
	return rr
}

func (dnsServer *KubeDockDns) forwardToUpstream(r *dns.Msg) *dns.Msg {
	c := new(dns.Client)
	resp, _, err := c.Exchange(r, dnsServer.upstreamDnsServer)
	if err != nil {
		log.Printf("Error forwarding to upstream: %v", err)
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeServerFailure)
		return m
	}
	return resp
}
