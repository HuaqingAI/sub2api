{{/*
Expand the chart name.
*/}}
{{- define "sub2api.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "sub2api.fullname" -}}
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
Create chart name and version as used by the chart label.
*/}}
{{- define "sub2api.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels.
*/}}
{{- define "sub2api.labels" -}}
helm.sh/chart: {{ include "sub2api.chart" . }}
{{ include "sub2api.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{/*
Selector labels.
*/}}
{{- define "sub2api.selectorLabels" -}}
app.kubernetes.io/name: {{ include "sub2api.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Application ServiceAccount name.
*/}}
{{- define "sub2api.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "sub2api.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "sub2api.appSecretName" -}}
{{- if .Values.secrets.app.existingSecret -}}
{{- .Values.secrets.app.existingSecret -}}
{{- else -}}
{{- printf "%s-app" (include "sub2api.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "sub2api.postgresql.fullname" -}}
{{- printf "%s-postgresql" (include "sub2api.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "sub2api.postgresql.secretName" -}}
{{- if .Values.secrets.postgresql.existingSecret -}}
{{- .Values.secrets.postgresql.existingSecret -}}
{{- else -}}
{{- printf "%s-postgresql" (include "sub2api.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "sub2api.redis.fullname" -}}
{{- printf "%s-redis" (include "sub2api.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "sub2api.redis.secretName" -}}
{{- if .Values.secrets.redis.existingSecret -}}
{{- .Values.secrets.redis.existingSecret -}}
{{- else -}}
{{- printf "%s-redis" (include "sub2api.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/*
Return a base64-encoded Secret data value. When upgrading an existing release,
reuse the existing key unless the user provided a new explicit value.
*/}}
{{- define "sub2api.secretData" -}}
{{- $root := .root -}}
{{- $secretName := .secretName -}}
{{- $key := .key -}}
{{- $value := default "" .value -}}
{{- $generated := default "" .generated -}}
{{- $existing := lookup "v1" "Secret" $root.Release.Namespace $secretName -}}
{{- if ne $value "" -}}
{{- $value | b64enc | quote -}}
{{- else if and $existing (hasKey $existing.data $key) -}}
{{- index $existing.data $key | quote -}}
{{- else -}}
{{- $generated | b64enc | quote -}}
{{- end -}}
{{- end -}}

