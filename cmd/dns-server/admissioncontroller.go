package main

import (
	"context"
	"fmt"
	"github.com/miekg/dns"
	"io"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"log"
	"net/http"
	"strconv"
	"wamblee.org/kubedock/dns/internal/support"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"encoding/json"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

const (
	CONTROLLER_NAME = "kubedock-admission"
)

type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

type DnsMutator struct {
	pods         *Pods
	dnsServiceIP string
	clientConfig *dns.ClientConfig
}

func NewDnsMutator(pods *Pods, dnsServiceIP string, clientConfig *dns.ClientConfig) *DnsMutator {
	mutator := DnsMutator{
		pods:         pods,
		dnsServiceIP: dnsServiceIP,
		clientConfig: clientConfig,
	}
	return &mutator
}

func (mutator *DnsMutator) handleMutate(w http.ResponseWriter, r *http.Request) {

	// NOTE: the mutator tries to gurantee a consisten network setup without conflicts, but
	// becasue of concurrency this is not possible. Conflicts can occur in two ways:
	// 1. errors in labeling or annotations by the user
	// 2. editing of annotations after deployment to make them invalid.
	//
	// The check of the network is just a best effort.

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		http.Error(w, "Invalid Content-Type", http.StatusUnsupportedMediaType)
		return
	}

	var admissionReview admissionv1.AdmissionReview
	if _, _, err := deserializer.Decode(body, nil, &admissionReview); err != nil {
		http.Error(w, fmt.Sprintf("Could not decode body: %v", err), http.StatusBadRequest)
		return
	}

	var k8spod corev1.Pod
	err = json.Unmarshal(admissionReview.Request.Object.Raw, &k8spod)
	if err != nil {
		http.Error(w, fmt.Sprintf("Could not unmarshal pod: %v", err), http.StatusBadRequest)
		return
	}

	log.Printf("Adding dnsconfig and policy to pod %s/%s", k8spod.Namespace, k8spod.Name)

	pod, err := getPodEssentials(&k8spod, "0.0.0.0")
	if err != nil {
		log.Printf("Pod is misconfigured: %v", err)
	} else {
		newpods := mutator.pods.Copy()
		newpods.AddOrUpdate(pod)
		_, podErrors := newpods.Networks()
		if podErrors != nil {
			podError := podErrors.FirstError(pod)
			if podError != nil {
				// reject deployment because the specific pod is giving an error,
				// here we ignore errors from other pods.
				// In this design, only pods with valid network config can be deployed,
				// so errors in other pods should never occur. However, metadata of pods
				// can be changed in running pods, causing errors there. We do not want errors
				// in other pods to influence deployment of valid pods.
				err = podError
			}
		}
	}

	var admissionResponse *admissionv1.AdmissionResponse
	if err != nil {
		admissionResponse = mutator.rejectPod(admissionReview, err)
	} else {
		admissionResponse = mutator.addDnsConfiguration(w, admissionReview)
		if admissionResponse == nil {
			return
		}
	}

	responseAdmissionReview := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Response: admissionResponse,
	}

	resp, err := json.Marshal(responseAdmissionReview)
	if err != nil {
		http.Error(w, fmt.Sprintf("Could not marshal response: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(resp)
}

func (mutator *DnsMutator) addDnsConfiguration(w http.ResponseWriter, admissionReview admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	var patches []PatchOperation

	ndots := strconv.Itoa(mutator.clientConfig.Ndots)
	timeout := strconv.Itoa(mutator.clientConfig.Timeout)
	attempts := strconv.Itoa(mutator.clientConfig.Attempts)
	patches = append(patches,
		PatchOperation{
			Op:    "add",
			Path:  "/spec/dnsPolicy",
			Value: "None",
		}, PatchOperation{
			Op:   "add",
			Path: "/spec/dnsConfig",
			Value: corev1.PodDNSConfig{
				Nameservers: []string{mutator.dnsServiceIP},
				Searches:    mutator.clientConfig.Search,
				Options: []corev1.PodDNSConfigOption{
					// TODO: examine whether "port" works
					{Name: "ndots", Value: &ndots},
					{Name: "timeout", Value: &timeout},
					{Name: "attempts", Value: &attempts},
				},
			},
		})

	// Create the patch bytes
	patchBytes, err := json.Marshal(patches)
	if err != nil {
		http.Error(w, fmt.Sprintf("Could not marshal patch: %v", err), http.StatusInternalServerError)
		return nil
	}

	// Create the admission response
	admissionResponse := admissionv1.AdmissionResponse{
		UID:     admissionReview.Request.UID,
		Allowed: true,
		Patch:   patchBytes,
		PatchType: func() *admissionv1.PatchType {
			pt := admissionv1.PatchTypeJSONPatch
			return &pt
		}(),
	}

	return &admissionResponse
}

func (mutator *DnsMutator) rejectPod(admissionReview admissionv1.AdmissionReview,
	err error) *admissionv1.AdmissionResponse {
	reviewResponse := admissionv1.AdmissionResponse{
		UID:     admissionReview.Request.UID,
		Allowed: false,
		Result: &metav1.Status{
			Status:  "Failure",
			Message: err.Error(),
			Reason:  metav1.StatusReasonConflict,
			Code:    http.StatusConflict,
		},
	}

	// Add audit annotations if needed
	reviewResponse.AuditAnnotations = map[string]string{
		"rejected-by": CONTROLLER_NAME,
		"reason":      "policy-violation",
	}
	return &reviewResponse
}

func runAdmisstionController(ctx context.Context,
	pods *Pods,
	clientset *kubernetes.Clientset,
	namespace string,
	dnsServiceName string,
	crtFile string,
	keyFile string) error {
	svc, err := clientset.CoreV1().Services(namespace).Get(ctx, dnsServiceName, v1.GetOptions{})
	if err != nil {
		return fmt.Errorf("Could not get dns service IP for service '%s'", dnsServiceName)
	}
	dnsServiceIP := svc.Spec.ClusterIP
	log.Printf("Service IP is %s", dnsServiceIP)

	dnsMutator := NewDnsMutator(pods, dnsServiceIP, support.GetClientConfig())

	http.HandleFunc("/mutate/pods", dnsMutator.handleMutate)
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	log.Printf("Starting webhook server on port 8443")
	return http.ListenAndServeTLS(":8443", crtFile, keyFile, nil)
}
