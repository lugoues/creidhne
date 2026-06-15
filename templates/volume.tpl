{{ template "unit" .Unit }}
{{ if .Volume -}}
[Volume]
{{ if .Volume.VolumeName -}}VolumeName={{ .Volume.VolumeName }}
{{ end -}}
{{ if .Volume.ServiceName -}}ServiceName={{ .Volume.ServiceName }}
{{ end -}}
{{ if .Volume.Driver -}}Driver={{ .Volume.Driver }}
{{ end -}}
{{ range .Volume.Options -}}Options={{ . }}
{{ end -}}
{{ if .Volume.Type -}}Type={{ .Volume.Type }}
{{ end -}}
{{ range .Volume.Device -}}Device={{ . }}
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
{{ range .Volume.Label -}}Label={{ . }}
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