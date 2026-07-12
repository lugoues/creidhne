{{ template "unit" .Unit }}
[Kube]
{{ range .Kube.Yaml -}}Yaml={{ . }}
{{ end -}}
{{ if .Kube.ServiceName -}}ServiceName={{ .Kube.ServiceName }}
{{ end -}}
{{ range .Kube.ConfigMap -}}ConfigMap={{ . }}
{{ end -}}
{{ range .Kube.AutoUpdate -}}AutoUpdate={{ . }}
{{ end -}}
{{ if .Kube.ExitCodePropagation -}}ExitCodePropagation={{ .Kube.ExitCodePropagation }}
{{ end -}}
{{ if .Kube.KubeDownForce -}}KubeDownForce=true
{{ end -}}
{{ if .Kube.LogDriver -}}LogDriver={{ .Kube.LogDriver }}
{{ end -}}
{{ range .Kube.LogOpt -}}LogOpt={{ . }}
{{ end -}}
{{ range .Kube.Network -}}Network={{ . }}
{{ end -}}
{{ range .Kube.PublishPort -}}PublishPort={{ . }}
{{ end -}}
{{ if .userNSString -}}UserNS={{ .userNSString }}
{{ end -}}
{{ if .Kube.SetWorkingDirectory -}}SetWorkingDirectory={{ .Kube.SetWorkingDirectory }}
{{ end -}}
{{ range .Kube.GlobalArgs -}}GlobalArgs={{ . }}
{{ end -}}
{{ range .Kube.PodmanArgs -}}PodmanArgs={{ . }}
{{ end -}}
{{ range .Kube.ContainersConfModule -}}ContainersConfModule={{ . }}
{{ end -}}
{{ if .Quadlet }}
[Quadlet]
DefaultDependencies={{ .Quadlet.DefaultDependencies }}
{{ end -}}
{{ template "service" .Service -}}
{{ template "install" .Install }}