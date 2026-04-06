package model

import (
	"testing"

	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"wamblee.org/kubedock/dns/internal/config"
)

// TestSuite for PodEssentials
type PodEssentialsSuite struct {
	suite.Suite
	podConfig config.PodConfig
}

func (s *PodEssentialsSuite) SetupSuite() {
	s.podConfig = config.PodConfig{
		HostAliasPrefix: "kubedock-host-",
		NetworkIdPrefix: "kubedock-net-",
		LabelName:       "kubedock-enabled",
	}
}

func (s *PodEssentialsSuite) TearDownSuite() {
	// nothing to clean up
}

func (s *PodEssentialsSuite) SetupTest() {
	// nothing per test
}

func (s *PodEssentialsSuite) TearDownTest() {
	// nothing per test
}

func TestPodEssentialsSuite(t *testing.T) {
	suite.Run(t, new(PodEssentialsSuite))
}

// createTestPod creates a test pod with the given name, IP, ready status, and deletion timestamp
func (s *PodEssentialsSuite) createTestPod(name string, ip string, ready bool, deletionTimestamp *metav1.Time) *corev1.Pod {
	readyStatus := corev1.ConditionFalse
	if ready {
		readyStatus = corev1.ConditionTrue
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
			Labels:    map[string]string{s.podConfig.LabelName: "true"},
			Annotations: map[string]string{
				s.podConfig.HostAliasPrefix + "example": "example.com",
				s.podConfig.NetworkIdPrefix + "net1":    "net1",
			},
			DeletionTimestamp: deletionTimestamp,
		},
		Status: corev1.PodStatus{
			PodIP: ip,
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: readyStatus,
			}},
		},
	}
}

// hostnamesToStrings converts a slice of Hostname to a slice of strings
func (s *PodEssentialsSuite) hostnamesToStrings(hostnames []Hostname) []string {
	out := make([]string, len(hostnames))
	for i, h := range hostnames {
		out[i] = string(h)
	}
	return out
}

// networkIdsToStrings converts a slice of NetworkId to a slice of strings
func (s *PodEssentialsSuite) networkIdsToStrings(ids []NetworkId) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = string(id)
	}
	return out
}

// TestGetPodEssentials_Success verifies that a correctly configured pod
// returns a Pod instance with the expected IP, host aliases, networks and
// readiness flag.
func (s *PodEssentialsSuite) TestGetPodEssentials_Success() {
	// Build a pod with required labels, annotations, IP and ready status
	pod := s.createTestPod("test-pod", "10.0.0.1", true, nil)

	// Call function and assert success
	p, err := GetPodEssentials(pod, "", s.podConfig)
	s.NoError(err)
	s.NotNil(p)
	s.Equal("10.0.0.1", string(p.IP))
	s.Equal([]string{"example.com"}, s.hostnamesToStrings(p.HostAliases))
	s.Equal([]string{"net1"}, s.networkIdsToStrings(p.Networks))
	s.True(p.Ready)
}

// TestGetPodEssentials_NoIP ensures that GetPodEssentials returns an error
// when the pod has no IP address.
func (s *PodEssentialsSuite) TestGetPodEssentials_NoIP() {
	// Pod missing IP should trigger an error
	pod := s.createTestPod("test-pod", "", true, nil)

	_, err := GetPodEssentials(pod, "", s.podConfig)
	s.Error(err)
}

// TestGetPodEssentials_Deleted verifies that a pod with a deletion timestamp
// is marked as not ready.
func (s *PodEssentialsSuite) TestGetPodEssentials_Deleted() {
	now := metav1.Now()
	pod := s.createTestPod("deleted-pod", "10.0.0.2", true, &now)

	p, err := GetPodEssentials(pod, "", s.podConfig)
	s.NoError(err)
	s.False(p.Ready)
}
