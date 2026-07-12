{{ template "unit" .Unit }}
{{ if .Pod -}}
[Pod]
{{ if .Pod.PodName -}}PodName={{ .Pod.PodName }}
{{ end -}}
{{ if .Pod.ServiceName -}}ServiceName={{ .Pod.ServiceName }}
{{ end -}}
{{ range .networkStrings -}}Network={{ . }}
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
{{ range .volumeStrings -}}Volume={{ . }}
{{ end -}}
{{ if .Pod.ShmSize -}}ShmSize={{ .Pod.ShmSize }}
{{ end -}}
{{ range .labelStrings -}}Label={{ . }}
{{ end -}}
{{ if .userNSString -}}UserNS={{ .userNSString }}
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