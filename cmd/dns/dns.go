package main

import (
	"log"
	"net"
	"os"
	"sync"

	"github.com/miekg/dns"
)

type KubeDockDns struct {
	mutex             sync.RWMutex
	networks          *Networks
	upstreamDnsServer string
}

func NewKubeDockDns(upstreamDnsServer string) *KubeDockDns {
	server := KubeDockDns{
		mutex:             sync.RWMutex{},
		networks:          NewNetworks(),
		upstreamDnsServer: upstreamDnsServer,
	}
	return &server
}

func (dns *KubeDockDns) SetNetworks(networks *Networks) {
	dns.mutex.Lock()
	defer dns.mutex.Unlock()

	dns.networks = networks
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
			sourceIp := IPAddress(os.Getenv("KUBEDOCK_DNS_SOURCE_IP"))
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
						Ttl:    300, // Time-to-live in seconds
					},
					A: net.ParseIP(string(ip)), // Example IP address
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

//
//func mainOld() {
//	clientConfig, err := dns.ClientConfigFromFile("/etc/resolv.conf")
//	if err != nil {
//		panic(err)
//	}
//	upstreamDnsServer := clientConfig.Servers[0]
//	log.Printf("DNS server %s", upstreamDnsServer)
//	kubedocDns := NewKubeDockDns(upstreamDnsServer + ":53")
//	kubedocDns.AddCname("postgres.ns.svc.cluster.local", "nu.nl")
//	kubedocDns.Serve()
//}
