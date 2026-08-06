{{ template "unit" .Unit }}
[Image]
Image={{ .Image.Image }}
{{ if .Image.ServiceName -}}ServiceName={{ .Image.ServiceName }}
{{ end -}}
{{ if isset .Image "AllTags" -}}AllTags={{ .Image.AllTags }}
{{ end -}}
{{ if .Image.Policy -}}Policy={{ .Image.Policy }}
{{ end -}}
{{ if .Image.Arch -}}Arch={{ .Image.Arch }}
{{ end -}}
{{ if .Image.OS -}}OS={{ .Image.OS }}
{{ end -}}
{{ if .Image.Variant -}}Variant={{ .Image.Variant }}
{{ end -}}
{{ if .Image.AuthFile -}}AuthFile={{ .Image.AuthFile }}
{{ end -}}
{{ if .Image.CertDir -}}CertDir={{ .Image.CertDir }}
{{ end -}}
{{ if .Image.Creds -}}Creds={{ .Image.Creds }}
{{ end -}}
{{ if .Image.DecryptionKey -}}DecryptionKey={{ .Image.DecryptionKey }}
{{ end -}}
{{ if isset .Image "TLSVerify" -}}TLSVerify={{ .Image.TLSVerify }}
{{ end -}}
{{ if .Image.ImageTag -}}ImageTag={{ .Image.ImageTag }}
{{ end -}}
{{ if isset .Image "Retry" -}}Retry={{ printf "%d" .Image.Retry }}
{{ end -}}
{{ if .Image.RetryDelay -}}RetryDelay={{ .Image.RetryDelay }}
{{ end -}}
{{ range .Image.GlobalArgs -}}GlobalArgs={{ . }}
{{ end -}}
{{ range .Image.PodmanArgs -}}PodmanArgs={{ . }}
{{ end -}}
{{ range .Image.ContainersConfModule -}}ContainersConfModule={{ . }}
{{ end -}}
{{ if .Quadlet }}
[Quadlet]
DefaultDependencies={{ .Quadlet.DefaultDependencies }}
{{ end -}}
{{ template "service" .Service -}}
{{ template "install" .Install }}