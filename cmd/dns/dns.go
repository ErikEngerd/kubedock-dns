package main

import (
	"log"
	"net"
	"sync"

	"github.com/miekg/dns"
)

type KubeDockDns struct {
	mutex             sync.RWMutex
	networks          *Networks
	upstreamDnsServer string

	overrideSourceIP IPAddress
}

func NewKubeDockDns(upstreamDnsServer string) *KubeDockDns {
	server := KubeDockDns{
		mutex:             sync.RWMutex{},
		networks:          NewNetworks(),
		upstreamDnsServer: upstreamDnsServer,
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
	server := &dns.Server{Addr: ":53", Net: "udp"}
	log.Printf("Starting DNS server on %s\n", server.Addr)
	err := server.ListenAndServe()
	defer server.Shutdown()
	if err != nil {
		log.Fatalf("Failed to start server: %s\n ", err.Error())
	}
}

func (dnsServer *KubeDockDns) handleDNSRequest(w dns.ResponseWriter, r *dns.Msg) {
	dnsServer.mutex.RLock()
	defer dnsServer.mutex.RUnlock()

	m := new(dns.Msg)
	m.SetReply(r)

	for _, question := range r.Question {
		if question.Qtype == dns.TypeA {
			sourceIp := dnsServer.overrideSourceIP
			if sourceIp == "" {
				sourceIp = IPAddress(w.RemoteAddr().String())
			}

			log.Printf("dns: A %s", question.Name)
			ip := dnsServer.networks.Lookup(
				sourceIp,
				Hostname(question.Name[:len(question.Name)-1]))

			if ip != "" {
				log.Printf("dns: %s -> %s", question.Name, ip)
				rr := &dns.A{
					Hdr: dns.RR_Header{
						Name:   question.Name,
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    300,
					},
					A: net.ParseIP(string(ip)),
				}
				m.Answer = append(m.Answer, rr)
				continue
			}
		}
		log.Printf("dns: forwarding question %+v to upstream", question)
		upstreamResponse := dnsServer.forwardToUpstream(r)
		m.Answer = append(m.Answer, upstreamResponse.Answer...)
	}

	w.WriteMsg(m)
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
