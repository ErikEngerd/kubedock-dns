package main

import (
	"context"
	"fmt"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"log"
	"net/http"
	"wamblee.org/kubedock/dns/internal/support"
)

func main() {
	fmt.Println("Hello world!")

	clientset, namespace := support.GetKubernetesConnection()
	ctx := context.Background()
	svc, err := clientset.CoreV1().Services(namespace).Get(ctx, "dns-server", v1.GetOptions{})
	if err != nil {
		log.Panicf("COuld not get dns service IP")
	}
	dnsServiceIP := svc.Spec.ClusterIP
	log.Printf("Service IP is %s", dnsServiceIP)

	dnsMutator := DnsMutator{
		dnsServiceIP: dnsServiceIP,
	}

	certFile := "/etc/kubedock/pki/tls.crt"
	keyFile := "/etc/kubedock/pki/tls.key"

	http.HandleFunc("/mutate/pods", dnsMutator.handleMutate)
	log.Printf("Starting webhook server on port 8443")
	if err := http.ListenAndServeTLS(":8443", certFile, keyFile, nil); err != nil {
		log.Fatal(err)
	}
}
