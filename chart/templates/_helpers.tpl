{{/*
Expand the name of the chart.
*/}}
{{- define "incident-commander.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "incident-commander.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "incident-commander.labels" -}}
helm.sh/chart: {{ include "incident-commander.chart" . }}
{{ include "incident-commander.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "incident-commander.selectorLabels" -}}
app.kubernetes.io/name: {{ include "incident-commander.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: incident-commander
{{- end }}
