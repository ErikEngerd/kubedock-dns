package main

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"log"
	"strconv"
	"testing"
)

func BenchmarkCreateNetworks(b *testing.B) {
	nPodsPerTest := 3
	nTests := 300

	pods := NewPods()

	for i := range nTests {
		for j := range nPodsPerTest {
			ipod := i*nPodsPerTest + j
			pod, err := NewPod(
				IPAddress(strconv.Itoa(ipod)),
				"kubedock",
				fmt.Sprintf("pod%d", ipod),
				[]Hostname{Hostname(fmt.Sprintf("host%d", j))},
				[]NetworkId{NetworkId(fmt.Sprintf("network%d", i))},
			)
			assert.Nil(b, err)
			pods.AddOrUpdate(pod)
		}
	}

	log.Printf("Created pods")
	b.ResetTimer()
	for _ = range b.N {
		//t0 := time.Now()
		_, err := pods.Networks()
		//dt := time.Now().Sub(t0)
		//s.Nil(err)
		assert.Nil(b, err)
	}
}
