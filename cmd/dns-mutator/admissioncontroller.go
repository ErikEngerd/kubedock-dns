package main

import (
	"fmt"
	"io"
	corev1 "k8s.io/api/core/v1"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"encoding/json"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

type DnsMutator struct {
	dnsServiceIP string
}

func (mutator *DnsMutator) handleMutate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		http.Error(w, "Invalid Content-Type", http.StatusUnsupportedMediaType)
		return
	}

	var admissionReview admissionv1.AdmissionReview
	if _, _, err := deserializer.Decode(body, nil, &admissionReview); err != nil {
		http.Error(w, fmt.Sprintf("Could not decode body: %v", err), http.StatusBadRequest)
		return
	}

	var pod corev1.Pod
	err = json.Unmarshal(admissionReview.Request.Object.Raw, &pod)
	if err != nil {
		http.Error(w, fmt.Sprintf("Could not unmarshal pod: %v", err), http.StatusBadRequest)
		return
	}

	// Create patch operations
	var patches []PatchOperation

	// Example mutation: Add a label
	patches = append(patches, PatchOperation{
		Op:    "add",
		Path:  "/metadata/labels/injected-by",
		Value: "admission-controller",
	})

	// Create the patch bytes
	patchBytes, err := json.Marshal(patches)
	if err != nil {
		http.Error(w, fmt.Sprintf("Could not marshal patch: %v", err), http.StatusInternalServerError)
		return
	}

	// Create the admission response
	admissionResponse := admissionv1.AdmissionResponse{
		UID:     admissionReview.Request.UID,
		Allowed: true,
		Patch:   patchBytes,
		PatchType: func() *admissionv1.PatchType {
			pt := admissionv1.PatchTypeJSONPatch
			return &pt
		}(),
	}

	// Create the admission review response
	responseAdmissionReview := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Response: &admissionResponse,
	}

	resp, err := json.Marshal(responseAdmissionReview)
	if err != nil {
		http.Error(w, fmt.Sprintf("Could not marshal response: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(resp)
}
