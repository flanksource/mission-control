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

{{/*
https://github.com/ory/k8s/blob/master/helm/charts/kratos/templates/_helpers.tpl#L103
*/}}

{{/*
Create a secret name which can be overridden.
*/}}
{{- define "kratos-im.secretname" -}}
{{- .Values.kratos.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Generate the secrets.default value
*/}}
{{- define "kratos-im.secrets.default" -}}
  {{- if (.Values.kratos.kratos.config.secrets).default -}}
    {{- if kindIs "slice" .Values.kratos.kratos.config.secrets.default -}}
      {{- if gt (len .Values.kratos.kratos.config.secrets.default) 1 -}}
        "{{- join "\",\"" .Values.kratos.kratos.config.secrets.default -}}"
      {{- else -}}
        {{- join "" .Values.kratos.kratos.config.secrets.default -}}
      {{- end -}}
    {{- else -}}
      {{- fail "Expected kratos.kratos.config.secrets.default to be a list of strings" -}}
    {{- end -}}
  {{- end -}}
{{- end -}}

{{/*
Generate the secrets.cookie value
*/}}
{{- define "kratos-im.secrets.cookie" -}}
  {{- if (.Values.kratos.kratos.config.secrets).cookie -}}
    {{- if kindIs "slice" .Values.kratos.kratos.config.secrets.cookie -}}
      {{- if gt (len .Values.kratos.kratos.config.secrets.cookie) 1 -}}
        "{{- join "\",\"" .Values.kratos.kratos.config.secrets.cookie -}}"
      {{- else -}}
        {{- join "" .Values.kratos.kratos.config.secrets.cookie -}}
      {{- end -}}
    {{- else -}}
      {{- fail "Expected kratos.config.secrets.cookie to be a list of strings" -}}
    {{- end -}}
  {{- end -}}
{{- end -}}

{{/*
Generate the secrets.cipher value
*/}}
{{- define "kratos-im.secrets.cipher" -}}
  {{- if (.Values.kratos.kratos.config.secrets).cipher -}}
    {{- if kindIs "slice" .Values.kratos.kratos.config.secrets.cipher -}}
      {{- if gt (len .Values.kratos.kratos.config.secrets.cipher) 1 -}}
        "{{- join "\",\"" .Values.kratos.kratos.config.secrets.cipher -}}"
      {{- else -}}
        {{- join "" .Values.kratos.kratos.config.secrets.cipher -}}
      {{- end -}}
    {{- else -}}
      {{- fail "Expected kratos.config.secrets.cipher to be a list of strings" -}}
    {{- end -}}
  {{- end -}}
{{- end -}}
