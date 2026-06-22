{{ template "unit" .Unit }}
[Container]
{{ if .Container.Image -}}Image={{ .Container.Image }}
{{ end -}}
{{ if .Container.Rootfs -}}Rootfs={{ .Container.Rootfs }}
{{ end -}}
{{ if .Container.ServiceName -}}ServiceName={{ .Container.ServiceName }}
{{ end -}}
{{ if .Container.ContainerName -}}ContainerName={{ .Container.ContainerName }}
{{ end -}}
{{ if .Container.Entrypoint -}}Entrypoint={{ .Container.Entrypoint }}
{{ end -}}
{{ if .Container.Exec -}}Exec={{ .Container.Exec }}
{{ end -}}
{{ if .Container.WorkingDir -}}WorkingDir={{ .Container.WorkingDir }}
{{ end -}}
{{ if .Container.User -}}User={{ .Container.User }}
{{ end -}}
{{ if .Container.Group -}}Group={{ .Container.Group }}
{{ end -}}
{{ if .Container.RunInit -}}RunInit=true
{{ end -}}
{{ range .Container.Environment -}}Environment={{ . }}
{{ end -}}
{{ range .Container.EnvironmentFile -}}EnvironmentFile={{ . }}
{{ end -}}
{{ if .Container.EnvironmentHost -}}EnvironmentHost=true
{{ end -}}
{{ if .Container.HttpProxy -}}HttpProxy=true
{{ end -}}
{{ range .Container.Network -}}Network={{ . }}
{{ end -}}
{{ range .Container.NetworkAlias -}}NetworkAlias={{ . }}
{{ end -}}
{{ range .Container.PublishPort -}}PublishPort={{ . }}
{{ end -}}
{{ range .Container.ExposeHostPort -}}ExposeHostPort={{ . }}
{{ end -}}
{{ if .Container.HostName -}}HostName={{ .Container.HostName }}
{{ end -}}
{{ if .Container.IP -}}IP={{ .Container.IP }}
{{ end -}}
{{ if .Container.IP6 -}}IP6={{ .Container.IP6 }}
{{ end -}}
{{ range .Container.DNS -}}DNS={{ . }}
{{ end -}}
{{ range .Container.DNSOption -}}DNSOption={{ . }}
{{ end -}}
{{ range .Container.DNSSearch -}}DNSSearch={{ . }}
{{ end -}}
{{ range .Container.AddHost -}}AddHost={{ . }}
{{ end -}}
{{ range .volumeStrings -}}Volume={{ . }}
{{ end -}}
{{ range .Container.Mount -}}Mount={{ . }}
{{ end -}}
{{ range .Container.Tmpfs -}}Tmpfs={{ . }}
{{ end -}}
{{ range .Container.AddCapability -}}AddCapability={{ . }}
{{ end -}}
{{ range .Container.DropCapability -}}DropCapability={{ . }}
{{ end -}}
{{ if .Container.NoNewPrivileges -}}NoNewPrivileges=true
{{ end -}}
{{ if .Container.SeccompProfile -}}SeccompProfile={{ .Container.SeccompProfile }}
{{ end -}}
{{ if .Container.AppArmor -}}AppArmor={{ .Container.AppArmor }}
{{ end -}}
{{ if .Container.SecurityLabelDisable -}}SecurityLabelDisable=true
{{ end -}}
{{ if .Container.SecurityLabelFileType -}}SecurityLabelFileType={{ .Container.SecurityLabelFileType }}
{{ end -}}
{{ if .Container.SecurityLabelType -}}SecurityLabelType={{ .Container.SecurityLabelType }}
{{ end -}}
{{ if .Container.SecurityLabelLevel -}}SecurityLabelLevel={{ .Container.SecurityLabelLevel }}
{{ end -}}
{{ if .Container.SecurityLabelNested -}}SecurityLabelNested=true
{{ end -}}
{{ if .Container.ReadOnly -}}ReadOnly=true
{{ end -}}
{{ if .Container.ReadOnlyTmpfs -}}ReadOnlyTmpfs=true
{{ end -}}
{{ if .Container.Mask -}}Mask={{ .Container.Mask }}
{{ end -}}
{{ if .Container.Unmask -}}Unmask={{ .Container.Unmask }}
{{ end -}}
{{ range .Container.AddDevice -}}AddDevice={{ . }}
{{ end -}}
{{ if .Container.Memory -}}Memory={{ .Container.Memory }}
{{ end -}}
{{ if .Container.PidsLimit -}}PidsLimit={{ .Container.PidsLimit }}
{{ end -}}
{{ range .Container.Ulimit -}}Ulimit={{ . }}
{{ end -}}
{{ if .Container.ShmSize -}}ShmSize={{ .Container.ShmSize }}
{{ end -}}
{{ range .Container.Label -}}Label={{ . }}
{{ end -}}
{{ range .Container.Annotation -}}Annotation={{ . }}
{{ end -}}
{{ if .Container.HealthCmd -}}HealthCmd={{ .Container.HealthCmd }}
{{ end -}}
{{ if .Container.HealthInterval -}}HealthInterval={{ .Container.HealthInterval }}
{{ end -}}
{{ if isset .Container "HealthRetries" -}}HealthRetries={{ printf "%d" .Container.HealthRetries }}
{{ end -}}
{{ if .Container.HealthStartPeriod -}}HealthStartPeriod={{ .Container.HealthStartPeriod }}
{{ end -}}
{{ if .Container.HealthTimeout -}}HealthTimeout={{ .Container.HealthTimeout }}
{{ end -}}
{{ if .Container.HealthOnFailure -}}HealthOnFailure={{ .Container.HealthOnFailure }}
{{ end -}}
{{ if .Container.HealthLogDestination -}}HealthLogDestination={{ .Container.HealthLogDestination }}
{{ end -}}
{{ if isset .Container "HealthMaxLogCount" -}}HealthMaxLogCount={{ printf "%d" .Container.HealthMaxLogCount }}
{{ end -}}
{{ if .Container.HealthMaxLogSize -}}HealthMaxLogSize={{ .Container.HealthMaxLogSize }}
{{ end -}}
{{ if .Container.HealthStartupCmd -}}HealthStartupCmd={{ .Container.HealthStartupCmd }}
{{ end -}}
{{ if .Container.HealthStartupInterval -}}HealthStartupInterval={{ .Container.HealthStartupInterval }}
{{ end -}}
{{ if isset .Container "HealthStartupRetries" -}}HealthStartupRetries={{ printf "%d" .Container.HealthStartupRetries }}
{{ end -}}
{{ if isset .Container "HealthStartupSuccess" -}}HealthStartupSuccess={{ printf "%d" .Container.HealthStartupSuccess }}
{{ end -}}
{{ if .Container.HealthStartupTimeout -}}HealthStartupTimeout={{ .Container.HealthStartupTimeout }}
{{ end -}}
{{ if .Container.LogDriver -}}LogDriver={{ .Container.LogDriver }}
{{ end -}}
{{ range .Container.LogOpt -}}LogOpt={{ . }}
{{ end -}}
{{ if .Container.StopSignal -}}StopSignal={{ .Container.StopSignal }}
{{ end -}}
{{ if isset .Container "StopTimeout" -}}StopTimeout={{ .Container.StopTimeout }}
{{ end -}}
{{ if isset .Container "Notify" -}}Notify={{ .Container.Notify }}
{{ end -}}
{{ if .Container.CgroupsMode -}}CgroupsMode={{ .Container.CgroupsMode }}
{{ end -}}
{{ if .Container.UserNS -}}UserNS={{ .Container.UserNS }}
{{ end -}}
{{ range .Container.UIDMap -}}UIDMap={{ . }}
{{ end -}}
{{ range .Container.GIDMap -}}GIDMap={{ . }}
{{ end -}}
{{ if .Container.SubUIDMap -}}SubUIDMap={{ .Container.SubUIDMap }}
{{ end -}}
{{ if .Container.SubGIDMap -}}SubGIDMap={{ .Container.SubGIDMap }}
{{ end -}}
{{ range .Container.GroupAdd -}}GroupAdd={{ . }}
{{ end -}}
{{ if .Container.Pod -}}Pod={{ .Container.Pod }}
{{ end -}}
{{ if .Container.StartWithPod -}}StartWithPod=true
{{ end -}}
{{ if .Container.AutoUpdate -}}AutoUpdate={{ .Container.AutoUpdate }}
{{ end -}}
{{ if .Container.Pull -}}Pull={{ .Container.Pull }}
{{ end -}}
{{ if isset .Container "Retry" -}}Retry={{ printf "%d" .Container.Retry }}
{{ end -}}
{{ if .Container.RetryDelay -}}RetryDelay={{ .Container.RetryDelay }}
{{ end -}}
{{ range .secretStrings -}}Secret={{ . }}
{{ end -}}
{{ range .Container.Sysctl -}}Sysctl={{ . }}
{{ end -}}
{{ if .Container.Timezone -}}Timezone={{ .Container.Timezone }}
{{ end -}}
{{ range .Container.GlobalArgs -}}GlobalArgs={{ . }}
{{ end -}}
{{ range .Container.PodmanArgs -}}PodmanArgs={{ . }}
{{ end -}}
{{ if .Container.ReloadCmd -}}ReloadCmd={{ .Container.ReloadCmd }}
{{ end -}}
{{ if .Container.ReloadSignal -}}ReloadSignal={{ .Container.ReloadSignal }}
{{ end -}}
{{ range .Container.ContainersConfModule -}}ContainersConfModule={{ . }}
{{ end -}}
{{ if .Quadlet }}
[Quadlet]
DefaultDependencies={{ .Quadlet.DefaultDependencies }}
{{ end -}}
{{ template "service" .Service -}}
{{ template "install" .Install }}