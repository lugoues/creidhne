{{ template "unit" .Unit }}
[Build]
{{ range .Build.ImageTag -}}ImageTag={{ . }}
{{ end -}}
{{ if .Build.ServiceName -}}ServiceName={{ .Build.ServiceName }}
{{ end -}}
{{ if .ContainerFile -}}
{{ if .contextPath -}}SetWorkingDirectory={{ .contextPath }}
{{ else -}}SetWorkingDirectory=unit
{{ end -}}
File={{ .containerfilePath }}
{{ else -}}
{{ if .Build.SetWorkingDirectory -}}SetWorkingDirectory={{ .Build.SetWorkingDirectory }}
{{ end -}}
{{ if .Build.File -}}File={{ .Build.File }}
{{ end -}}
{{ end -}}
{{ if .Build.IgnoreFile -}}IgnoreFile={{ .Build.IgnoreFile }}
{{ end -}}
{{ if .Build.Target -}}Target={{ .Build.Target }}
{{ end -}}
{{ range .Build.BuildArg -}}BuildArg={{ . }}
{{ end -}}
{{ range .Build.Environment -}}Environment={{ . }}
{{ end -}}
{{ if .Build.Arch -}}Arch={{ .Build.Arch }}
{{ end -}}
{{ if .Build.Variant -}}Variant={{ .Build.Variant }}
{{ end -}}
{{ if .Build.AuthFile -}}AuthFile={{ .Build.AuthFile }}
{{ end -}}
{{ range .Build.networkStrings -}}Network={{ . }}
{{ end -}}
{{ range .Build.DNS -}}DNS={{ . }}
{{ end -}}
{{ range .Build.DNSOption -}}DNSOption={{ . }}
{{ end -}}
{{ range .Build.DNSSearch -}}DNSSearch={{ . }}
{{ end -}}
{{ range .Build.labelStrings -}}Label={{ . }}
{{ end -}}
{{ range .Build.Annotation -}}Annotation={{ . }}
{{ end -}}
{{ if .Build.ForceRM -}}ForceRM=true
{{ end -}}
{{ if .Build.Pull -}}Pull={{ .Build.Pull }}
{{ end -}}
{{ if .Build.TLSVerify -}}TLSVerify=true
{{ end -}}
{{ range .Build.Secret -}}Secret={{ . }}
{{ end -}}
{{ range .Build.volumeStrings -}}Volume={{ . }}
{{ end -}}
{{ range .Build.GroupAdd -}}GroupAdd={{ . }}
{{ end -}}
{{ if isset .Build "Retry" -}}Retry={{ printf "%d" .Build.Retry }}
{{ end -}}
{{ if .Build.RetryDelay -}}RetryDelay={{ .Build.RetryDelay }}
{{ end -}}
{{ range .Build.GlobalArgs -}}GlobalArgs={{ . }}
{{ end -}}
{{ range .Build.PodmanArgs -}}PodmanArgs={{ . }}
{{ end -}}
{{ range .Build.ContainersConfModule -}}ContainersConfModule={{ . }}
{{ end -}}
{{ if .Quadlet }}
[Quadlet]
DefaultDependencies={{ .Quadlet.DefaultDependencies }}
{{ end -}}
{{ template "service" .Service -}}
{{ template "install" .Install }}