package support

import (
	"github.com/miekg/dns"
	"regexp"
)

func GetClientConfig() *dns.ClientConfig {
	clientConfig, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil {
		panic(err)
	}
	return clientConfig
}

func IsValidHostname(hostname string) bool {
	if len(hostname) > 255 {
		return false
	}
	pattern := `^([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]*[a-zA-Z0-9])(\.([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]*[a-zA-Z0-9]))*$`
	matched, _ := regexp.MatchString(pattern, hostname)
	return matched
}
