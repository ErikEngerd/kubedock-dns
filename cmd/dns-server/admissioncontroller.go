package main

import (
	"context"
	"fmt"
	"github.com/miekg/dns"
	"gomodules.xyz/jsonpatch/v2"
	"io"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"log"
	"net/http"
	"strconv"
	"time"
	"wamblee.org/kubedock/dns/internal/support"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"encoding/json"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
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

func NewDnsMutator(decoder runtime.Decoder, pods *Pods, dnsServiceIP string, clientConfig *dns.ClientConfig) *DnsMutator {
	mutator := DnsMutator{
		pods:         pods,
		dnsServiceIP: dnsServiceIP,
		clientConfig: clientConfig,
	}
	return &mutator
}

func (mutator *DnsMutator) errored(code int32, err error) admission.Response {
	log.Printf("Error: %d: %v", code, err)
	return admission.Errored(code, err)
}

func (mutator *DnsMutator) Handle(ctx context.Context, request admission.Request) admission.Response {
	log.Println("Inside handle")

	var k8spod corev1.Pod
	err := json.Unmarshal(request.Object.Raw, &k8spod)
	if err != nil {
		return mutator.errored(http.StatusBadRequest, fmt.Errorf("Could not unmarshal pod: %v", err))
	}

	log.Printf("Adding dnsconfig and policy to pod %s/%s", k8spod.Namespace, k8spod.Name)

	err = mutator.validateK8sPod(k8spod, request.Operation)
	if err != nil {
		return mutator.rejectPod(request, err)
	}
	return mutator.addDnsConfiguration(request)
}

func (mutator *DnsMutator) handleMutate(w http.ResponseWriter, r *http.Request) {
	log.Println("Inside mutator")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	log.Println("Read body")
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		http.Error(w, "Invalid Content-Type", http.StatusUnsupportedMediaType)
		return
	}

	log.Println("verified content type")

	var admissionReview admissionv1.AdmissionReview
	admissionRequest := admission.Request{}
	admissionReview.Request = &admissionRequest.AdmissionRequest
	if err := json.Unmarshal(body, &admissionReview); err != nil {
		log.Printf("Error unmarshalling json: %v\n%s", err, string(body))
		http.Error(w, fmt.Sprintf("Could not decode body: %v", err), http.StatusBadRequest)
		return
	}

	log.Println("After parsing request")

	admissionResponse := mutator.Handle(context.Background(), admissionRequest)
	if err := admissionResponse.Complete(admissionRequest); err != nil {
		log.Printf("unable to encode response: %v", err)
		admissionResponse = mutator.errored(http.StatusInternalServerError, err)
		// Note: We explicitly have to set the response UID. Usually that is done via resp.Complete.
		admissionResponse.UID = admissionRequest.UID
	}

	responseAdmissionReview := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Response: &admissionResponse.AdmissionResponse,
	}

	resp, err := json.Marshal(responseAdmissionReview)
	if err != nil {
		http.Error(w, fmt.Sprintf("Could not marshal response: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(resp)
}

func (mutator *DnsMutator) validateK8sPod(k8spod corev1.Pod, operation admissionv1.Operation) error {
	// add pod with an unknown IP indicator but with a unique IP. The IP will be updated
	// later when the IP becomes known during deployment.
	podIpOverride := k8spod.Status.PodIP
	if podIpOverride == "" {
		podIpOverride = UNKNOWN_IP_PREFIX + strconv.Itoa(time.Now().Nanosecond()) +
			strconv.Itoa(rand.Int())
	}
	pod, err := getPodEssentials(&k8spod, podIpOverride)
	if err != nil {
		return err
	}
	var networks *Networks
	networks, err = mutator.validatePod(operation, pod)
	if err != nil {
		return err
	}
	log.Printf("Pod %s/%s was valid", pod.Namespace, pod.Name)
	networks.Log()
	return nil
}

func (mutator *DnsMutator) validatePod(operation admissionv1.Operation, pod *Pod) (*Networks, error) {
	if operation == admissionv1.Update {
		oldpod := mutator.pods.Get(pod.Namespace, pod.Name)
		if !oldpod.Equal(pod) {
			return nil, fmt.Errorf("%s/%s: cannot change network configuration after creation",
				pod.Namespace, pod.Name)
		}
	}
	mutator.pods.AddOrUpdate(pod)

	// Because of concurrency, other pods can have been added concurrently
	// But the order of adding pods to the network is deterministic because
	// of how LinkedMap works by adding all pods to the nwtwork in the same
	// order one by one.
	//
	// reject deployment because the specific pod is giving an error,
	// here we ignore errors from other pods.
	// In this design, only pods with valid network config can be deployed,
	// so errors in other pods should never occur. However, metadata of pods
	// can be changed in running pods, causing errors there. We do not want errors
	// in other pods to influence deployment of valid pods.
	networks, podErrors := mutator.pods.Networks()
	if podErrors == nil {
		return networks, nil
	}
	podError := podErrors.FirstError(pod)
	if podError == nil {
		return networks, nil
	}

	mutator.pods.Delete(pod.Namespace, pod.Name)
	return nil, podError
}

func (mutator *DnsMutator) addDnsConfiguration(request admission.Request) admission.Response {

	ndots := strconv.Itoa(mutator.clientConfig.Ndots)
	timeout := strconv.Itoa(mutator.clientConfig.Timeout)
	attempts := strconv.Itoa(mutator.clientConfig.Attempts)
	patches := []jsonpatch.JsonPatchOperation{
		{
			Operation: "add",
			Path:      "/spec/dnsPolicy",
			Value:     "None",
		},
		{
			Operation: "add",
			Path:      "/spec/dnsConfig",
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
		},
	}

	// Create the admission response
	response := admission.Response{
		Patches: patches,
		AdmissionResponse: admissionv1.AdmissionResponse{
			Allowed: true,
			PatchType: func() *admissionv1.PatchType {
				pt := admissionv1.PatchTypeJSONPatch
				return &pt
			}(),
		},
	}
	return response
}

func (mutator *DnsMutator) rejectPod(request admission.Request,
	err error) admission.Response {
	response := admission.Response{
		AdmissionResponse: admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Status:  "Failure",
				Message: err.Error(),
				Reason:  metav1.StatusReasonConflict,
				Code:    http.StatusConflict,
			},
			AuditAnnotations: map[string]string{
				"rejected-by": CONTROLLER_NAME,
				"reason":      "policy-violation",
			},
		},
	}
	return response
}

func runAdmisstionController(ctx context.Context,
	pods *Pods,
	clientset *kubernetes.Clientset,
	namespace string,
	dnsServiceName string,
	crtFile string,
	keyFile string) error {

	scheme := runtime.NewScheme()
	//if err := corev1.AddToScheme(scheme); err != nil {
	//	log.Printf("unable to add corev1 schema: %v", err)
	//	os.Exit(1)
	//}
	//decoder := admission.NewDecoder(scheme)

	codecs := serializer.NewCodecFactory(scheme)
	decoder := codecs.UniversalDeserializer()

	svc, err := clientset.CoreV1().Services(namespace).Get(ctx, dnsServiceName, v1.GetOptions{})
	if err != nil {
		return fmt.Errorf("Could not get dns service IP for service '%s'", dnsServiceName)
	}
	dnsServiceIP := svc.Spec.ClusterIP
	log.Printf("Service IP is %s", dnsServiceIP)

	dnsMutator := NewDnsMutator(decoder, pods, dnsServiceIP, support.GetClientConfig())

	http.HandleFunc("/mutate/pods", dnsMutator.handleMutate)
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	log.Printf("Starting webhook server on port 8443")
	return http.ListenAndServeTLS(":8443", crtFile, keyFile, nil)
}
