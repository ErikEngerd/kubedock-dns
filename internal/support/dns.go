package support

import "github.com/miekg/dns"

func GetClientConfig() *dns.ClientConfig {
	clientConfig, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil {
		panic(err)
	}
	return clientConfig
}
