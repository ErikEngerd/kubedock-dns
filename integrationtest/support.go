package integrationtest

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/gruntwork-io/terratest/modules/helm"
	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/stretchr/testify/require"
)

func HelmReleases(t *testing.T, options *k8s.KubectlOptions, namespace string) []string {
	helmOptions := &helm.Options{
		KubectlOptions: options,
	}
	listJson, listJsonErr, err := helm.RunHelmCommandAndGetStdOutErrE(t, helmOptions, "list", "--all", "-o", "json", "-n", namespace)
	require.Nil(t, err, listJsonErr)
	var list []map[string]string
	err = json.Unmarshal([]byte(listJson), &list)
	require.Nil(t, err, listJson)
	fmt.Printf("\n\n%+v\n\n", list)
	releases := []string{}
	for _, release := range list {
		releases = append(releases, release["name"])
	}
	return releases
}

func MapSlice[From any, To any](in []From, mapper func(From) To) []To {
	res := make([]To, len(in))
	for ind := range in {
		res[ind] = mapper(in[ind])
	}
	return res
}

func HelmChartName() string {
	name := os.Getenv("HELM_CHART")
	if name != "" {
		return name
	}
	return "../helm/kubedock-dns"
}
