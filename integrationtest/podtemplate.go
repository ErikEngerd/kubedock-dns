package integrationtest

import (
	"embed"
	"fmt"
	"html/template"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

//go:embed podtemplate/pod.yaml
var podTemplateFS embed.FS

// PodTemplateValues represents the template values used in pod.yaml
type PodTemplateValues struct {
	Name                   string
	Enabled                string
	Ready                  bool
	HostAliases            []string
	LabelName              string
	Networks               []string
	ShutdownTimeoutSeconds int
}

type PodOption func(values PodTemplateValues) PodTemplateValues

func DisableKubedock(values PodTemplateValues) PodTemplateValues {
	values.Enabled = "false"
	return values
}

func NotReady(values PodTemplateValues) PodTemplateValues {
	values.Ready = false
	return values
}

func AddNetwork(network string) PodOption {
	return func(values PodTemplateValues) PodTemplateValues {
		values.Networks = append(values.Networks, network)
		return values
	}
}

func AddHostAlias(host string) PodOption {
	return func(values PodTemplateValues) PodTemplateValues {
		values.HostAliases = append(values.HostAliases, host)
		return values
	}
}

// CreatePodFromTemplate creates a *v1.Pod using go templating
func CreatePodFromTemplate(values PodTemplateValues) (*v1.Pod, error) {
	// Read the template file
	templateContent, err := podTemplateFS.ReadFile("podtemplate/pod.yaml")
	if err != nil {
		return nil, err
	}

	// Parse the template
	tmpl, err := template.New("pod").Parse(string(templateContent))
	if err != nil {
		return nil, err
	}

	// Execute the template with the provided values
	var result strings.Builder
	if err := tmpl.Execute(&result, values); err != nil {
		return nil, err
	}

	// Parse the resulting YAML into a v1.Pod
	var pod v1.Pod
	if err := yaml.Unmarshal([]byte(result.String()), &pod); err != nil {
		return nil, err
	}

	return &pod, nil
}

func CreatePod(name string, host string, network string, options ...PodOption) (*v1.Pod, error) {
	values := PodTemplateValues{
		Name:                   name,
		Enabled:                "true",
		Ready:                  true,
		HostAliases:            []string{host},
		LabelName:              "kubedock",
		Networks:               []string{network},
		ShutdownTimeoutSeconds: 5,
	}
	for _, option := range options {
		values = option(values)
	}
	fmt.Printf("OPtions: %+v\n", values)
	return CreatePodFromTemplate(values)
}
