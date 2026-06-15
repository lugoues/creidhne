{{ template "unit" .Unit }}
[Artifact]
Artifact={{ .Artifact.Artifact }}
{{ if .Artifact.ServiceName -}}ServiceName={{ .Artifact.ServiceName }}
{{ end -}}
{{ if .Artifact.AuthFile -}}AuthFile={{ .Artifact.AuthFile }}
{{ end -}}
{{ if .Artifact.CertDir -}}CertDir={{ .Artifact.CertDir }}
{{ end -}}
{{ if .Artifact.Creds -}}Creds={{ .Artifact.Creds }}
{{ end -}}
{{ if .Artifact.DecryptionKey -}}DecryptionKey={{ .Artifact.DecryptionKey }}
{{ end -}}
{{ if .Artifact.TLSVerify -}}TLSVerify=true
{{ end -}}
{{ if .Artifact.Quiet -}}Quiet=true
{{ end -}}
{{ if isset .Artifact "Retry" -}}Retry={{ printf "%d" .Artifact.Retry }}
{{ end -}}
{{ if .Artifact.RetryDelay -}}RetryDelay={{ .Artifact.RetryDelay }}
{{ end -}}
{{ range .Artifact.GlobalArgs -}}GlobalArgs={{ . }}
{{ end -}}
{{ range .Artifact.PodmanArgs -}}PodmanArgs={{ . }}
{{ end -}}
{{ range .Artifact.ContainersConfModule -}}ContainersConfModule={{ . }}
{{ end -}}
{{ if .Quadlet }}
[Quadlet]
DefaultDependencies={{ .Quadlet.DefaultDependencies }}
{{ end -}}
{{ template "service" .Service -}}
{{ template "install" .Install }}