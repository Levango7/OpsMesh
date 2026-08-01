{{/*
Expand the name of the chart.
*/}}
{{- define "opsmesh.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this length.
*/}}
{{- define "opsmesh.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Chart name and version label.
*/}}
{{- define "opsmesh.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels shared by all OpsMesh resources.
*/}}
{{- define "opsmesh.labels" -}}
helm.sh/chart: {{ include "opsmesh.chart" . }}
{{ include "opsmesh.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels (must be subset of common labels; immutable across upgrades).
*/}}
{{- define "opsmesh.selectorLabels" -}}
app.kubernetes.io/name: {{ include "opsmesh.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Service account name.
*/}}
{{- define "opsmesh.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "opsmesh.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Image reference: optional registry prefix + repository:tag.
Usage: {{ include "opsmesh.image" (list . .Values.controlplane.image) }}
*/}}
{{- define "opsmesh.image" -}}
{{- $root := index . 0 -}}
{{- $img := index . 1 -}}
{{- if $root.Values.global.imageRegistry -}}
{{- printf "%s/%s:%s" $root.Values.global.imageRegistry $img.repository $img.tag -}}
{{- else -}}
{{- printf "%s:%s" $img.repository $img.tag -}}
{{- end -}}
{{- end -}}

{{/*
Controlplane fully qualified service name (used by agent to discover controlplane).
*/}}
{{- define "opsmesh.controlplane.serviceName" -}}
{{- printf "%s-controlplane" (include "opsmesh.fullname" .) -}}
{{- end -}}

{{/*
MySQL service name.
*/}}
{{- define "opsmesh.mysql.serviceName" -}}
{{- printf "%s-mysql" (include "opsmesh.fullname" .) -}}
{{- end -}}

{{/*
Redis service name.
*/}}
{{- define "opsmesh.redis.serviceName" -}}
{{- printf "%s-redis" (include "opsmesh.fullname" .) -}}
{{- end -}}

{{/*
Agent headless service name.
*/}}
{{- define "opsmesh.agent.serviceName" -}}
{{- printf "%s-agent" (include "opsmesh.fullname" .) -}}
{{- end -}}

{{/*
Storage class helper: empty string falls back to cluster default (omit field).
Usage: {{ include "opsmesh.storageClass" .Values.mysql.persistence.storageClass }}
*/}}
{{- define "opsmesh.storageClass" -}}
{{- if . -}}
storageClassName: {{ . | quote }}
{{- end -}}
{{- end -}}

{{/*
Image pull secrets list.
*/}}
{{- define "opsmesh.imagePullSecrets" -}}
{{- if .Values.global.imagePullSecrets -}}
imagePullSecrets:
{{- range .Values.global.imagePullSecrets }}
  - name: {{ .name }}
{{- end -}}
{{- end -}}
{{- end -}}