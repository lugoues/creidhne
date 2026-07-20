{{ template "unit" .Unit }}
{{ if .Volume -}}
[Volume]
{{ if .Volume.VolumeName -}}VolumeName={{ .Volume.VolumeName }}
{{ end -}}
{{ if .Volume.ServiceName -}}ServiceName={{ .Volume.ServiceName }}
{{ end -}}
{{ if .Volume.Driver -}}Driver={{ .Volume.Driver }}
{{ end -}}
{{ if .Volume.Options -}}Options={{ range $i, $o := .Volume.Options }}{{ if $i }},{{ end }}{{ $o }}{{ end }}
{{ end -}}
{{ if .Volume.Type -}}Type={{ .Volume.Type }}
{{ end -}}
{{ if .Volume.Device -}}Device={{ .Volume.Device }}
{{ end -}}
Copy={{ .Volume.Copy }}
{{ if .Volume.Image -}}Image={{ .Volume.Image }}
{{ end -}}
{{ if .Volume.User -}}User={{ .Volume.User }}
{{ end -}}
{{ if .Volume.Group -}}Group={{ .Volume.Group }}
{{ end -}}
{{ if isset .Volume "UID" -}}UID={{ printf "%d" .Volume.UID }}
{{ end -}}
{{ if isset .Volume "GID" -}}GID={{ printf "%d" .Volume.GID }}
{{ end -}}
{{ range .labelStrings -}}Label={{ . }}
{{ end -}}
{{ range .Volume.GlobalArgs -}}GlobalArgs={{ . }}
{{ end -}}
{{ range .Volume.PodmanArgs -}}PodmanArgs={{ . }}
{{ end -}}
{{ range .Volume.ContainersConfModule -}}ContainersConfModule={{ . }}
{{ end -}}
{{ else -}}
[Volume]
{{ end -}}
{{ if .Quadlet }}
[Quadlet]
DefaultDependencies={{ .Quadlet.DefaultDependencies }}
{{ end -}}
{{ template "service" .Service -}}
{{ template "install" .Install }}