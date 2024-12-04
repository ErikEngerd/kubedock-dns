package main

import "time"

type PodConfig struct {
	HostAliasPrefix string
	NetworkIdPrefix string
	LabelName       string
}

type Config struct {
	PodConfig       PodConfig
	CrtFile         string
	KeyFile         string
	InternalDomains []string

	// Time that instrumented pods will wait until a record becomes available.
	// This is required since a container may do a DNS lookup so quickly after
	// startup that it is not yet known with the DNS server.
	DnsTimeout time.Duration

	// Number of retries that pods should do before failing a DNS lookup.
	DnsRetries int
}
