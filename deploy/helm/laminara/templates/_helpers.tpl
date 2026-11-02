{{- define "laminara.name" -}}
{{- printf "%s-laminara" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "laminara.image" -}}
{{- if .Values.image.digest -}}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository (required "Задайте image.tag из VERSION либо image.digest" .Values.image.tag) -}}
{{- end -}}
{{- end -}}
{{- define "laminara.serviceAccount" -}}
{{- if .Values.serviceAccount.create -}}
{{- .Values.serviceAccount.name | default (include "laminara.name" .) -}}
{{- else -}}
{{- .Values.serviceAccount.name | default "default" -}}
{{- end -}}
{{- end -}}
