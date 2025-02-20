package admissioncontroller

import (
	"context"
	"encoding/json"
	"github.com/miekg/dns"
	"github.com/stretchr/testify/suite"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"math/rand"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"strconv"
	"strings"
	"testing"
	config2 "wamblee.org/kubedock/dns/internal/config"
	"wamblee.org/kubedock/dns/internal/model"
)

type MutatorTestSuite struct {
	suite.Suite

	config config2.PodConfig

	ctx          context.Context
	dnsip        string
	pods         *model.Pods
	mutator      *DnsMutator
	clientConfig dns.ClientConfig
	stdlabels    map[string]string
}

func (s *MutatorTestSuite) SetupSuite() {
	s.config = config2.PodConfig{
		LabelName:       "kubedock",
		HostAliasPrefix: "kubedock.host/",
		NetworkIdPrefix: "kubedock.network/",
	}
}

func (s *MutatorTestSuite) SetupTest() {
	s.ctx = context.Background()
	s.dnsip = "10.11.12.13"
	s.pods = model.NewPods()
	s.clientConfig = dns.ClientConfig{
		Servers:  []string{"11.12.13.14"},
		Search:   []string{"a.b.c", "b.c", "c"},
		Port:     ":53",
		Ndots:    5,
		Timeout:  10,
		Attempts: 3,
	}
	s.mutator = NewDnsMutator(s.pods, s.dnsip, &s.clientConfig, s.config)
	s.stdlabels = map[string]string{
		"kubedock": "true",
	}
}

func (s *MutatorTestSuite) TearDownTest() {
}

func TestMutatorTestSuite(t *testing.T) {
	suite.Run(t, &MutatorTestSuite{})
}

func (s *MutatorTestSuite) createPod(namespace string, name string,
	annotations map[string]string,
	labels map[string]string,
	ip string) v1.Pod {
	if ip == "" {
		ip = model.UNKNOWN_IP_PREFIX + strconv.Itoa(rand.Int())
	}
	pod := v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
			Labels:      labels,
		},
		Spec: v1.PodSpec{},
		Status: v1.PodStatus{
			PodIP: ip,
		},
	}
	return pod
}

func (s *MutatorTestSuite) createRequest(
	operation admissionv1.Operation,
	name string,
	annotations map[string]string,
	labels map[string]string,
	ip string) admission.Request {

	namespace := "kubedock"

	pod := s.createPod(namespace, name, annotations, labels, ip)
	podRaw, err := json.Marshal(pod)
	s.Require().Nil(err)

	uid := strconv.Itoa(rand.Int())
	request := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID: types.UID(uid),
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			Resource: metav1.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "pods",
			},
			RequestKind: &metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			RequestResource: &metav1.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "pods",
			},
			Name:      name,
			Namespace: namespace,
			Operation: operation,
			Object: runtime.RawExtension{
				Raw: podRaw,
			},
		},
	}
	return request
}

func (s *MutatorTestSuite) marshall(object any) string {
	res, err := json.MarshalIndent(object, "", "  ")
	s.Nil(err)
	return string(res)
}

func (s *MutatorTestSuite) assertMutated(request admission.Request, response admission.Response) {
	ndotsString := strconv.Itoa(s.clientConfig.Ndots)
	timeoutString := strconv.Itoa(s.clientConfig.Timeout)
	attemptsString := strconv.Itoa(s.clientConfig.Attempts)
	patches := []PatchOperation{
		{
			Op:    "add",
			Path:  "/spec/dnsPolicy",
			Value: "None",
		},
		{
			Op:   "add",
			Path: "/spec/dnsConfig",
			Value: corev1.PodDNSConfig{
				Nameservers: []string{s.dnsip},
				Searches:    s.clientConfig.Search,
				Options: []corev1.PodDNSConfigOption{
					{Name: "ndots", Value: &ndotsString},
					{Name: "timeout", Value: &timeoutString},
					{Name: "attempts", Value: &attemptsString},
				},
			},
		},
	}
	patchesJson, err := json.Marshal(patches)
	s.Nil(err)
	var expectedPatches []any
	s.Nil(json.Unmarshal(patchesJson, &expectedPatches))

	var actualPatches []any
	s.Nil(json.Unmarshal(response.Patch, &actualPatches))
	s.Equal(expectedPatches, actualPatches)

	s.True(response.Allowed)
	s.Equal(request.UID, response.UID)
	s.Equal(int32(200), int32(response.Result.Code))
	s.Equal("JSONPatch", string(*response.PatchType))
}

func (s *MutatorTestSuite) Test_SingleHostAndNetwork() {
	request := s.createRequest("CREATE", "db",
		map[string]string{
			"kubedock.host/0":    "db",
			"kubedock.network/0": "test",
		},
		map[string]string{
			"kubedock": "true",
		},
		"20.21.22.23")
	response := s.mutator.Handle(s.ctx, request)
	s.Nil(response.Complete(request))

	s.assertMutated(request, response)

	pod := s.pods.Get("kubedock", "db")
	s.NotNil(pod)
	s.Equal([]model.Hostname{"db"}, pod.HostAliases)
	s.Equal([]model.NetworkId{"test"}, pod.Networks)
}

func (s *MutatorTestSuite) Test_DuplicateHost() {
	s.Test_SingleHostAndNetwork()

	// add another pod in the same network with same hostname
	request := s.createRequest("CREATE", "db2",
		map[string]string{
			"kubedock.host/0":    "db",
			"kubedock.network/0": "test",
		},
		map[string]string{
			"kubedock": "true",
		},
		"20.21.22.23")
	response := s.mutator.Handle(s.ctx, request)
	s.True(response.Allowed)

	s.NotNil(s.pods.Get("kubedock", "db"))
	s.NotNil(s.pods.Get("kubedock", "db2"))
}

func (s *MutatorTestSuite) Test_SecondHost() {
	s.Test_SingleHostAndNetwork()

	// add another pod in the same network with same hostname
	request := s.createRequest("CREATE", "service",
		map[string]string{
			"kubedock.host/0":    "service",
			"kubedock.network/0": "test",
		},
		map[string]string{
			"kubedock": "true",
		},
		"20.21.22.23")
	response := s.mutator.Handle(s.ctx, request)
	response.Complete(request)
	s.assertMutated(request, response)

	s.NotNil(s.pods.Get("kubedock", "db"))
	s.NotNil(s.pods.Get("kubedock", "service"))
}

func (s *MutatorTestSuite) Test_MissingHostname() {
	// add another pod in the same network with same hostname
	request := s.createRequest("CREATE", "db2",
		map[string]string{
			"kubedock.network/0": "test",
		},
		map[string]string{
			"kubedock": "true",
		},
		"20.21.22.23")
	response := s.mutator.Handle(s.ctx, request)
	s.False(response.Allowed)

	klog.V(3).Infof("Message: %s", response.Result.Message)
	s.True(strings.Contains(response.Result.Message, "no host or no network"))

	s.Nil(s.pods.Get("kubedock", "db"))
}

func (s *MutatorTestSuite) Test_MissingNetwork() {
	// add another pod in the same network with same hostname
	request := s.createRequest("CREATE", "db2",
		map[string]string{
			"kubedock.host/0": "db",
		},
		map[string]string{
			"kubedock": "true",
		},
		"20.21.22.23")
	response := s.mutator.Handle(s.ctx, request)
	s.False(response.Allowed)

	klog.V(3).Infof("Message: %s", response.Result.Message)
	s.True(strings.Contains(response.Result.Message, "no host or no network"))

	s.Nil(s.pods.Get("kubedock", "db"))
}

func (s *MutatorTestSuite) Test_MissingLabel() {
	// add another pod in the same network with same hostname
	request := s.createRequest("CREATE", "db2",
		map[string]string{
			"kubedock.host/0":    "db",
			"kubedock.network/0": "test",
		},
		map[string]string{},
		"20.21.22.23")
	response := s.mutator.Handle(s.ctx, request)
	s.False(response.Allowed)

	klog.V(3).Infof("Message: %s", response.Result.Message)
	s.True(strings.Contains(response.Result.Message, "does not have label"))

	s.Nil(s.pods.Get("kubedock", "db"))
}

func (s *MutatorTestSuite) Test_UpdateAllowedWhenNetworkNotModified() {
	s.Test_SingleHostAndNetwork()
	request := s.createRequest("UPDATE", "db",
		map[string]string{
			"kubedock.host/0":    "db",
			"kubedock.network/0": "test",
			"anotherannotation":  "anothervalue",
		},
		map[string]string{
			"kubedock":     "true",
			"anotherlabel": "anothervalue2",
		},
		"20.21.22.23")
	response := s.mutator.Handle(s.ctx, request)
	s.Nil(response.Complete(request))

	s.assertMutated(request, response)

	pod := s.pods.Get("kubedock", "db")
	s.NotNil(pod)
	s.Equal([]model.Hostname{"db"}, pod.HostAliases)
	s.Equal([]model.NetworkId{"test"}, pod.Networks)
}

func (s *MutatorTestSuite) Test_UpdateDeniedWhenHostModified() {
	s.Test_SingleHostAndNetwork()

	request := s.createRequest("UPDATE", "db",
		map[string]string{
			"kubedock.host/0":    "db2",
			"kubedock.network/0": "test",
		},
		map[string]string{
			"kubedock": "true",
		},
		"20.21.22.23")
	response := s.mutator.Handle(s.ctx, request)
	s.Nil(response.Complete(request))

	s.False(response.Allowed)
	klog.V(3).Infof("Message: %s", response.Result.Message)
	s.True(strings.Contains(response.Result.Message, "cannot change network"))
}

func (s *MutatorTestSuite) Test_UpdateDeniedWhenNetworkModified() {
	s.Test_SingleHostAndNetwork()

	request := s.createRequest("UPDATE", "db",
		map[string]string{
			"kubedock.host/0":    "db",
			"kubedock.network/0": "test2",
		},
		map[string]string{
			"kubedock": "true",
		},
		"20.21.22.23")
	response := s.mutator.Handle(s.ctx, request)
	s.Nil(response.Complete(request))

	s.False(response.Allowed)
	klog.V(3).Infof("Message: %s", response.Result.Message)
	s.True(strings.Contains(response.Result.Message, "cannot change network"))
}
