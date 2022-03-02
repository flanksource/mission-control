{{/*
Expand the name of the chart.
*/}}
{{- define "apm-hub.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}


{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "apm-hub.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "apm-hub.labels" -}}
helm.sh/chart: {{ include "apm-hub.chart" . }}
{{ include "apm-hub.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "apm-hub.selectorLabels" -}}
app.kubernetes.io/name: {{ include "apm-hub.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: {{ .Chart.Name }}
{{- end }}
