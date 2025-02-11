package main

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"k8s.io/klog/v2"
	"strconv"
	"testing"
	"wamblee.org/kubedock/dns/internal/model"
)

func BenchmarkCreateNetworks(b *testing.B) {
	nPodsPerTest := 3
	nTests := 300

	pods := model.NewPods()

	for i := range nTests {
		for j := range nPodsPerTest {
			ipod := i*nPodsPerTest + j
			pod, err := model.NewPod(
				model.IPAddress(strconv.Itoa(ipod)),
				"kubedock",
				fmt.Sprintf("pod%d", ipod),
				[]model.Hostname{model.Hostname(fmt.Sprintf("host%d", j))},
				[]model.NetworkId{model.NetworkId(fmt.Sprintf("network%d", i))},
				true,
			)
			assert.Nil(b, err)
			pods.AddOrUpdate(pod)
		}
	}

	klog.V(3).Infof("Created pods")
	b.ResetTimer()
	for _ = range b.N {
		//t0 := time.Now()
		_, err := pods.Networks()
		//dt := time.Now().Sub(t0)
		//s.Nil(err)
		assert.Nil(b, err)
	}
}
