package integrationtest

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/gruntwork-io/terratest/modules/retry"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type TestPod struct {
	s       *suite.Suite
	options *k8s.KubectlOptions
	pod     *v1.Pod
}

func NewTestPod(s *suite.Suite, kubectlOptions *k8s.KubectlOptions, name string, host string, network string,
	options ...PodOption) *TestPod {
	pod, err := CreatePod(name, host, network, options...)
	s.Require().Nil(err)
	return &TestPod{
		s:       s,
		options: kubectlOptions,
		pod:     pod,
	}
}

func (p *TestPod) Deploy() {
	_, err := p.clientset().CoreV1().Pods(p.options.Namespace).Create(context.Background(), p.pod, metav1.CreateOptions{})
	p.s.Require().Nil(err)
	p.WaitUntilRunning()
}

func (p *TestPod) Lookup(host string) []string {
	out, err := k8s.RunKubectlAndGetOutputE(p.s.T(), p.options, "exec", p.pod.Name, "--", "./dig_wrapper.sh", host)
	p.s.Require().Nil(err)
	out = strings.TrimSpace(out)
	res := strings.Split(out, "\n")
	res = slices.DeleteFunc(res, func(s string) bool {
		return s == ""
	})
	return res
}

func (p *TestPod) Delete() {
	err := p.clientset().CoreV1().Pods(p.options.Namespace).Delete(context.Background(), p.pod.Name, metav1.DeleteOptions{})
	p.s.Require().Nil(err)
}

func (p *TestPod) clientset() *kubernetes.Clientset {
	clientset, err := k8s.GetKubernetesClientFromOptionsE(p.s.T(), p.options)
	p.s.Require().Nil(err)
	return clientset
}

func (p *TestPod) IPAddress() string {
	return p.getPod().Status.PodIP
}

func (p *TestPod) SetReady(ready bool) {
	if ready {
		k8s.RunKubectl(p.s.T(), p.options, "exec", p.pod.Name, "--", "touch", "ready")
		p.WaitUntilReady()
	} else {
		k8s.RunKubectl(p.s.T(), p.options, "exec", p.pod.Name, "--", "rm", "-f", "ready")
		p.WaitUntilNotReady()
	}
}

func (p *TestPod) WaitUntilRunning() {
	retry.DoWithRetry(p.s.T(), "wait until running", 20, 1*time.Second, func() (string, error) {
		pod := p.getPod()
		if pod.Status.Phase != v1.PodRunning {
			return "", fmt.Errorf("Pod %s not yet running", p.pod.Name)
		}
		return "", nil
	})
}

func (p *TestPod) getPod() *v1.Pod {
	pod, err := p.clientset().CoreV1().Pods(p.options.Namespace).Get(context.Background(), p.pod.Name, metav1.GetOptions{})
	p.s.Require().Nil(err)
	return pod
}

func (p *TestPod) WaitUntilReady() {
	k8s.WaitUntilPodAvailable(p.s.T(), p.options, p.pod.Name, 20, 1*time.Second)
}

func (p *TestPod) WaitUntilNotReady() {
	retry.DoWithRetry(p.s.T(), "wait until unavailable", 20, 1*time.Second, func() (string, error) {
		if k8s.IsPodAvailable(p.getPod()) {
			return "", fmt.Errorf("Pod %s is still ready", p.pod.Name)
		}
		return "", nil
	})
}
