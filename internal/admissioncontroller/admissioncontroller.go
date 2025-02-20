package admissioncontroller

import (
	"context"
	"fmt"
	"github.com/miekg/dns"
	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"strconv"
	"time"
	"wamblee.org/kubedock/dns/internal/config"
	"wamblee.org/kubedock/dns/internal/model"
	"wamblee.org/kubedock/dns/internal/support"

	"encoding/json"
	controllerlog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	CONTROLLER_NAME = "kubedock-admission"
)

type DnsMutator struct {
	podConfig    config.PodConfig
	pods         *model.Pods
	dnsServiceIP string
	clientConfig *dns.ClientConfig
}

type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func NewDnsMutator(pods *model.Pods, dnsServiceIP string, clientConfig *dns.ClientConfig,
	podConfig config.PodConfig) *DnsMutator {
	mutator := DnsMutator{
		podConfig:    podConfig,
		pods:         pods,
		dnsServiceIP: dnsServiceIP,
		clientConfig: clientConfig,
	}
	return &mutator
}

func (mutator *DnsMutator) errored(code int32, err error) admission.Response {
	klog.Errorf("Error: %d: %v", code, err)
	return admission.Errored(code, err)
}

func (mutator *DnsMutator) Handle(ctx context.Context, request admission.Request) admission.Response {
	var k8spod corev1.Pod
	err := json.Unmarshal(request.Object.Raw, &k8spod)
	if err != nil {
		return mutator.errored(http.StatusBadRequest, fmt.Errorf("Could not unmarshal pod: %v", err))
	}
	err = mutator.validateK8sPod(k8spod, request.Operation)
	if err != nil {

		return mutator.rejectPod(request, err)
	}
	return mutator.addDnsConfiguration(request)
}

func (mutator *DnsMutator) validateK8sPod(k8spod corev1.Pod, operation admissionv1.Operation) error {
	// add pod with an unknown IP indicator but with a unique IP. The IP will be updated
	// later when the IP becomes known during deployment.
	podIpOverride := k8spod.Status.PodIP
	if podIpOverride == "" {
		podIpOverride = model.UNKNOWN_IP_PREFIX + strconv.Itoa(time.Now().Nanosecond()) +
			strconv.Itoa(rand.Int())
	}
	pod, err := model.GetPodEssentials(&k8spod, podIpOverride, mutator.podConfig)
	if err != nil {
		klog.Infof("%v", err)
		return err
	}
	var networks *model.Networks
	networks, err = mutator.validatePod(operation, pod)
	if err != nil {
		klog.Warningf("%s/%s invalid", pod.Namespace, pod.Name)
		return err
	}
	if klog.V(3).Enabled() {
		networks.Log()
	}
	return nil
}

func (mutator *DnsMutator) validatePod(operation admissionv1.Operation, pod *model.Pod) (*model.Networks, error) {
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
	// so errors in other pods should never occur.
	//
	// With more than one replica we cannot 100% guarantee that invalid pods will
	// be rejected, but in practice it should be close to 100%
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
	klog.Infof("%s/%s Adding dnsconfig", request.Namespace, request.Name)
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

func RunAdmisstionController(ctx context.Context,
	pods *model.Pods,
	clientset *kubernetes.Clientset,
	namespace string,
	dnsServiceName string,
	crtFile string,
	keyFile string,
	podConfig config.PodConfig) error {

	svc, err := clientset.CoreV1().Services(namespace).Get(ctx, dnsServiceName, v1.GetOptions{})
	if err != nil {
		return fmt.Errorf("Could not get dns service IP for service '%s': %v", dnsServiceName, err)
	}
	dnsServiceIP := svc.Spec.ClusterIP
	klog.Infof("DNS service IP is %s", dnsServiceIP)

	dnsMutator := NewDnsMutator(pods, dnsServiceIP, support.GetClientConfig(), podConfig)
	controllerlog.SetLogger(zap.New())

	webhook := admission.Webhook{
		Handler: dnsMutator,
	}

	dnsMutatorHandler, err := admission.StandaloneWebhook(&webhook, admission.StandaloneOptions{})
	if err != nil {
		return fmt.Errorf("Could not create mutator: %v", err)
	}

	http.HandleFunc("/mutate/pods", dnsMutatorHandler.ServeHTTP)
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	klog.Info("Starting webhook server on port 8443")
	return http.ListenAndServeTLS(":8443", crtFile, keyFile, nil)
}
