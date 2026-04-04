
{{- define "labels" }}
app.kubernetes.io/name: kubedock-dns
{{- end }}

{{/*
fullname returns .Values.fullnameOverride when set, otherwise .Release.Name.
The result is truncated to 50 characters and trailing dashes are removed.
50 is chosen so that the longest derived name in this chart ("<fullname>-mutator-cert",
+13 characters) never exceeds the Kubernetes 63-character name limit.
Use this everywhere a resource name is derived from the release name so that
operators can deploy multiple instances without naming collisions.
*/}}
{{- define "fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 50 | trimSuffix "-" -}}
{{- else -}}
{{- .Release.Name | trunc 50 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
