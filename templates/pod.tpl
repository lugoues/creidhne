{{ template "unit" .Unit }}
{{ if .Pod -}}
[Pod]
{{ if .Pod.PodName -}}PodName={{ .Pod.PodName }}
{{ end -}}
{{ if .Pod.ServiceName -}}ServiceName={{ .Pod.ServiceName }}
{{ end -}}
{{ range .Pod.Network -}}Network={{ . }}
{{ end -}}
{{ range .Pod.NetworkAlias -}}NetworkAlias={{ . }}
{{ end -}}
{{ range .Pod.PublishPort -}}PublishPort={{ . }}
{{ end -}}
{{ if .Pod.HostName -}}HostName={{ .Pod.HostName }}
{{ end -}}
{{ if .Pod.IP -}}IP={{ .Pod.IP }}
{{ end -}}
{{ if .Pod.IP6 -}}IP6={{ .Pod.IP6 }}
{{ end -}}
{{ range .Pod.DNS -}}DNS={{ . }}
{{ end -}}
{{ range .Pod.DNSOption -}}DNSOption={{ . }}
{{ end -}}
{{ range .Pod.DNSSearch -}}DNSSearch={{ . }}
{{ end -}}
{{ range .Pod.AddHost -}}AddHost={{ . }}
{{ end -}}
{{ range .Pod.Volume -}}Volume={{ . }}
{{ end -}}
{{ if .Pod.ShmSize -}}ShmSize={{ .Pod.ShmSize }}
{{ end -}}
{{ range .Pod.Label -}}Label={{ . }}
{{ end -}}
{{ if .Pod.UserNS -}}UserNS={{ .Pod.UserNS }}
{{ end -}}
{{ range .Pod.UIDMap -}}UIDMap={{ . }}
{{ end -}}
{{ range .Pod.GIDMap -}}GIDMap={{ . }}
{{ end -}}
{{ if .Pod.SubUIDMap -}}SubUIDMap={{ .Pod.SubUIDMap }}
{{ end -}}
{{ if .Pod.SubGIDMap -}}SubGIDMap={{ .Pod.SubGIDMap }}
{{ end -}}
{{ if .Pod.ExitPolicy -}}ExitPolicy={{ .Pod.ExitPolicy }}
{{ end -}}
{{ if isset .Pod "StopTimeout" -}}StopTimeout={{ .Pod.StopTimeout }}
{{ end -}}
{{ range .Pod.GlobalArgs -}}GlobalArgs={{ . }}
{{ end -}}
{{ range .Pod.PodmanArgs -}}PodmanArgs={{ . }}
{{ end -}}
{{ range .Pod.ContainersConfModule -}}ContainersConfModule={{ . }}
{{ end -}}
{{ else -}}
[Pod]
{{ end -}}
{{ if .Quadlet }}
[Quadlet]
DefaultDependencies={{ .Quadlet.DefaultDependencies }}
{{ end -}}
{{ template "service" .Service -}}
{{ template "install" .Install }}