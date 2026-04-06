package integrationtest

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/helm"
	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/gruntwork-io/terratest/modules/retry"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
)

// IntegrationTestSuite defines the test suite for integration tests
type IntegrationTestSuite struct {
	suite.Suite

	kubectlOptions *k8s.KubectlOptions
	clientset      *kubernetes.Clientset
}

// SetupSuite runs once before all tests in the suite
func (s *IntegrationTestSuite) SetupSuite() {
	// Initialize resources needed for all tests
}

// TearDownSuite runs once after all tests in the suite
func (s *IntegrationTestSuite) TearDownSuite() {
	// Clean up resources after all tests
}

// SetupTest runs before each test
func (s *IntegrationTestSuite) SetupTest() {
	// Check for allow-testing configmap

	namespace := "kubedock-dns-test"
	// Create kubectl options
	s.kubectlOptions = &k8s.KubectlOptions{
		Namespace: namespace,
	}
	if _, err := k8s.GetNamespaceE(s.T(), s.kubectlOptions, namespace); err != nil {
		k8s.CreateNamespace(s.T(), s.kubectlOptions, namespace)
	}

	var err error
	s.clientset, err = k8s.GetKubernetesClientFromOptionsE(s.T(), s.kubectlOptions)
	s.Require().Nil(err)

	// ConfigMap exists, delete all pods in the namespace
	s.T().Logf("Deleting all pods in namespace %s", namespace)

	s.cleanup(err)

	// install kubedock-dns
	// TODO; helm chart name must be configurable
	helm.Install(s.T(), &helm.Options{
		KubectlOptions: s.kubectlOptions,
		ExtraArgs: map[string][]string{
			"install": {"--wait"},
		},
	}, HelmChartName(), "dns")
}

func (s *IntegrationTestSuite) cleanup(err error) {
	// uninstall all helm charts
	helmreleases := HelmReleases(s.T(), s.kubectlOptions, s.kubectlOptions.Namespace)
	helmOptions := &helm.Options{
		KubectlOptions: s.kubectlOptions,
		ExtraArgs: map[string][]string{
			"delete": {"--wait"},
		},
	}
	for _, helmRelease := range helmreleases {
		helm.Delete(s.T(), helmOptions, helmRelease, true)
	}

	// Get list of pods
	pods := s.listPods()
	// Delete each pod using kubectl delete
	zero := int64(0)
	for _, pod := range pods {
		s.clientset.CoreV1().Pods(s.kubectlOptions.Namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{
			GracePeriodSeconds: &zero,
		})
	}

	retry.DoWithRetry(s.T(), "namespace empty", 40, 1*time.Second, func() (string, error) {
		pods := s.listPods()
		if len(pods) > 0 {
			podNames := MapSlice(pods, func(p v1.Pod) string {
				return p.Name
			})
			return "", fmt.Errorf("Pods still exist: %v", podNames)
		}
		return "", nil
	})
}

func (s *IntegrationTestSuite) deletePod(pod v1.Pod) {
	err := k8s.RunKubectlE(s.T(), s.kubectlOptions, "delete", "pod", pod.Name)
	s.Require().Nil(err)
}

func (s *IntegrationTestSuite) listPods() []v1.Pod {
	pods, err := k8s.ListPodsE(s.T(), s.kubectlOptions, metav1.ListOptions{})
	s.Require().Nil(err)
	return pods
}

// TearDownTest runs after each test
func (s *IntegrationTestSuite) TearDownTest() {
	// Clean up resources after each test
}

// TestIntegrationTestSuite runs the integration test suite
func TestIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}

func (s *IntegrationTestSuite) deployPod(pod *v1.Pod) *v1.Pod {
	pod, err := s.clientset.CoreV1().Pods(s.kubectlOptions.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	s.Require().Nil(err)
	return pod
}

// Test_Dummy is a simple empty test case to verify the test suite works
func (s *IntegrationTestSuite) XTest_PodTemplatingForTest() {
	pod, err := CreatePod("db1", "database", "network1", AddNetwork("pietjepuk"))
	s.Require().Nil(err)
	s.T().Logf("Successfully created pod: %s", pod.Name)
	s.T().Logf("Pod labels: %v", pod.Labels)
	s.T().Logf("Pod annotations: %v", pod.Annotations)

	serializer := json.NewSerializerWithOptions(
		json.DefaultMetaFactory, scheme.Scheme, scheme.Scheme,
		json.SerializerOptions{Yaml: true, Pretty: true},
	)
	buf := bytes.Buffer{}
	serializer.Encode(pod, &buf)
	s.T().Logf("Pod template\n%s\n", buf.String())

	s.deployPod(pod)
	service, err := CreatePod("service1", "service", "network1")
	s.Require().Nil(err)
	s.deployPod(service)

	k8s.WaitUntilPodAvailable(s.T(), s.kubectlOptions, pod.Name, 10, 1*time.Second)
	k8s.WaitUntilPodAvailable(s.T(), s.kubectlOptions, service.Name, 10, 1*time.Second)
}

func (s *IntegrationTestSuite) Test_NetworkIsolation() {
	// Single network

	db := NewTestPod(&s.Suite, s.kubectlOptions, "db1", "database", "network1")
	db.Deploy()
	server := NewTestPod(&s.Suite, s.kubectlOptions, "server1", "server", "network1")
	server.Deploy()

	s.Require().Equal([]string{server.IPAddress()}, db.Lookup("server"))
	s.Require().Equal([]string{db.IPAddress()}, db.Lookup("database"))
	s.Require().Equal([]string{db.IPAddress()}, server.Lookup("database"))
	s.Require().Equal([]string{server.IPAddress()}, server.Lookup("server"))

	// more pods, second testcase with same containers

	db2 := NewTestPod(&s.Suite, s.kubectlOptions, "db2", "database", "network2")
	db2.Deploy()
	server2 := NewTestPod(&s.Suite, s.kubectlOptions, "server2", "server", "network2")
	server2.Deploy()

	// old lookups keep on working
	s.Require().Equal([]string{server.IPAddress()}, db.Lookup("server"))
	s.Require().Equal([]string{db.IPAddress()}, db.Lookup("database"))
	s.Require().Equal([]string{db.IPAddress()}, server.Lookup("database"))
	s.Require().Equal([]string{server.IPAddress()}, server.Lookup("server"))

	// new lookups work as well within network2
	s.Require().Equal([]string{server2.IPAddress()}, db2.Lookup("server"))
	s.Require().Equal([]string{db2.IPAddress()}, db2.Lookup("database"))
	s.Require().Equal([]string{db2.IPAddress()}, server2.Lookup("database"))
	s.Require().Equal([]string{server2.IPAddress()}, server2.Lookup("server"))
}

func (s *IntegrationTestSuite) Test_NotReadyPodCannotBeResolve() {
	db := NewTestPod(&s.Suite, s.kubectlOptions, "db1", "database", "network1", NotReqdyAtStartup)
	db.Deploy()
	server := NewTestPod(&s.Suite, s.kubectlOptions, "server1", "server", "network1")
	server.Deploy()

	s.Require().Equal([]string{}, server.Lookup("database"))

	db.SetReady(true)
	s.Require().Equal([]string{db.IPAddress()}, server.Lookup("database"))
}

func (s *IntegrationTestSuite) Test_ResolutionWaitsABitUntilPodBecomesReady() {
	db := NewTestPod(&s.Suite, s.kubectlOptions, "db1", "database", "network1", NotReqdyAtStartup)
	db.Deploy()
	server := NewTestPod(&s.Suite, s.kubectlOptions, "server1", "server", "network1")
	server.Deploy()

	s.Require().Equal([]string{}, server.Lookup("database"))

	// pod becomes ready after we start the lookup
	go func() {
		time.Sleep(1 * time.Second)
		db.SetReady(true)
	}()
	s.Require().Equal([]string{db.IPAddress()}, server.Lookup("database"))
}

func (s *IntegrationTestSuite) Test_PodInMultipleNetworks() {
	server1 := NewTestPod(&s.Suite, s.kubectlOptions, "server1", "server1", "network1")
	server1.Deploy()
	server2 := NewTestPod(&s.Suite, s.kubectlOptions, "server2", "server2", "network2")
	server2.Deploy()
	client := NewTestPod(&s.Suite, s.kubectlOptions, "client", "client", "network1", AddNetwork("network2"))
	client.Deploy()

	// client can resolve both servers
	s.Require().Equal([]string{server1.IPAddress()}, client.Lookup("server1"))
	s.Require().Equal([]string{server2.IPAddress()}, client.Lookup("server2"))
}

func (s *IntegrationTestSuite) Test_PodBeingDeletedCannotBeResolved() {
	db := NewTestPod(&s.Suite, s.kubectlOptions, "db1", "database", "network1", ShutdownTimeout(30))
	db.Deploy()
	server := NewTestPod(&s.Suite, s.kubectlOptions, "server1", "server", "network1")
	server.Deploy()

	s.Require().Equal([]string{db.IPAddress()}, server.Lookup("database"))

	db.Delete(false)
	retry.DoWithRetry(s.T(), "wait until not resolvable", 10, 1*time.Second, func() (string, error) {
		lookup := server.Lookup("database")
		if len(lookup) > 0 {
			return "", fmt.Errorf("Can still lookup database")
		}
		return "", nil
	})
}
