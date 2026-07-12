{{ template "unit" .Unit }}
{{ if .Network -}}
[Network]
{{ if .Network.NetworkName -}}NetworkName={{ .Network.NetworkName }}
{{ end -}}
{{ if .Network.ServiceName -}}ServiceName={{ .Network.ServiceName }}
{{ end -}}
{{ if .Network.Driver -}}Driver={{ .Network.Driver }}
{{ end -}}
{{ if .Network.IPAMDriver -}}IPAMDriver={{ .Network.IPAMDriver }}
{{ end -}}
{{ range .Network.Options -}}Options={{ . }}
{{ end -}}
{{ if .Network.InterfaceName -}}InterfaceName={{ .Network.InterfaceName }}
{{ end -}}
{{ range .Network.Subnet -}}Subnet={{ . }}
{{ end -}}
{{ range .Network.Gateway -}}Gateway={{ . }}
{{ end -}}
{{ range .Network.IPRange -}}IPRange={{ . }}
{{ end -}}
{{ if .Network.IPv6 -}}IPv6=true
{{ end -}}
{{ if .Network.Internal -}}Internal=true
{{ end -}}
{{ if .Network.DisableDNS -}}DisableDNS=true
{{ end -}}
{{ if .Network.NetworkDeleteOnStop -}}NetworkDeleteOnStop=true
{{ end -}}
{{ range .Network.DNS -}}DNS={{ . }}
{{ end -}}
{{ range .labelStrings -}}Label={{ . }}
{{ end -}}
{{ range .Network.GlobalArgs -}}GlobalArgs={{ . }}
{{ end -}}
{{ range .Network.PodmanArgs -}}PodmanArgs={{ . }}
{{ end -}}
{{ range .Network.ContainersConfModule -}}ContainersConfModule={{ . }}
{{ end -}}
{{ else -}}
[Network]
{{ end -}}
{{ if .Quadlet }}
[Quadlet]
DefaultDependencies={{ .Quadlet.DefaultDependencies }}
{{ end -}}
{{ template "service" .Service -}}
{{ template "install" .Install }}