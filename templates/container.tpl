{{ if .Unit -}}
[Unit]
{{ if .Unit.Description -}}Description={{ .Unit.Description }}
{{ end -}}
{{ if .Unit.Documentation -}}Documentation={{ .Unit.Documentation }}
{{ end -}}
{{ range .Unit.After -}}After={{ . }}
{{ end -}}
{{ range .Unit.Before -}}Before={{ . }}
{{ end -}}
{{ range .Unit.Requires -}}Requires={{ . }}
{{ end -}}
{{ range .Unit.Wants -}}Wants={{ . }}
{{ end -}}
{{ range .Unit.BindsTo -}}BindsTo={{ . }}
{{ end -}}
{{ range .Unit.PartOf -}}PartOf={{ . }}
{{ end -}}
{{ range .Unit.Conflicts -}}Conflicts={{ . }}
{{ end -}}
{{ range .Unit.Condition -}}Condition={{ . }}
{{ end -}}
{{ range .Unit.Assert -}}Assert={{ . }}
{{ end -}}
{{ if .Unit.SourcePath -}}SourcePath={{ .Unit.SourcePath }}
{{ end -}}
{{ if .Unit.StopWhenUnneeded -}}StopWhenUnneeded=true
{{ end -}}
{{ if .Unit.RefuseManualStart -}}RefuseManualStart=true
{{ end -}}
{{ if .Unit.RefuseManualStop -}}RefuseManualStop=true
{{ end -}}
{{ if .Unit.AllowIsolate -}}AllowIsolate=true
{{ end -}}
{{ if .Unit.IgnoreOnIsolate -}}IgnoreOnIsolate=true
{{ end -}}
{{ if .Unit.OnSuccess -}}OnSuccess={{ .Unit.OnSuccess }}
{{ end -}}
{{ if .Unit.OnFailure -}}OnFailure={{ .Unit.OnFailure }}
{{ end -}}
{{ if .Unit.OnSuccessJobMode -}}OnSuccessJobMode={{ .Unit.OnSuccessJobMode }}
{{ end -}}
{{ if .Unit.OnFailureJobMode -}}OnFailureJobMode={{ .Unit.OnFailureJobMode }}
{{ end -}}
{{ if .Unit.StartLimitIntervalSec -}}StartLimitIntervalSec={{ .Unit.StartLimitIntervalSec }}
{{ end -}}
{{ if isset .Unit "StartLimitBurst" -}}StartLimitBurst={{ printf "%d" .Unit.StartLimitBurst }}
{{ end -}}
{{ end }}
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
{{ range .Container.Volume -}}Volume={{ . }}
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
{{ if .Service }}
[Service]
{{ if .Service.Type -}}Type={{ .Service.Type }}
{{ end -}}
{{ if .Service.Restart -}}Restart={{ .Service.Restart }}
{{ end -}}
{{ if .Service.RestartSec -}}RestartSec={{ .Service.RestartSec }}
{{ end -}}
{{ if .Service.TimeoutStartSec -}}TimeoutStartSec={{ .Service.TimeoutStartSec }}
{{ end -}}
{{ if .Service.TimeoutStopSec -}}TimeoutStopSec={{ .Service.TimeoutStopSec }}
{{ end -}}
{{ if .Service.TimeoutSec -}}TimeoutSec={{ .Service.TimeoutSec }}
{{ end -}}
{{ range .Service.ExecStartPre -}}ExecStartPre={{ . }}
{{ end -}}
{{ range .Service.ExecStartPost -}}ExecStartPost={{ . }}
{{ end -}}
{{ range .Service.ExecStop -}}ExecStop={{ . }}
{{ end -}}
{{ range .Service.ExecStopPost -}}ExecStopPost={{ . }}
{{ end -}}
{{ range .Service.ExecReload -}}ExecReload={{ . }}
{{ end -}}
{{ if .Service.WatchdogSec -}}WatchdogSec={{ .Service.WatchdogSec }}
{{ end -}}
{{ if .Service.RemainAfterExit -}}RemainAfterExit=true
{{ end -}}
{{ if .Service.KillMode -}}KillMode={{ .Service.KillMode }}
{{ end -}}
{{ if .Service.KillSignal -}}KillSignal={{ .Service.KillSignal }}
{{ end -}}
{{ if .Service.NotifyAccess -}}NotifyAccess={{ .Service.NotifyAccess }}
{{ end -}}
{{ if .Service.RestartPreventExitStatus -}}RestartPreventExitStatus={{ .Service.RestartPreventExitStatus }}
{{ end -}}
{{ if .Service.SuccessExitStatus -}}SuccessExitStatus={{ .Service.SuccessExitStatus }}
{{ end -}}
{{ range .Service.Environment -}}Environment={{ . }}
{{ end -}}
{{ range .Service.EnvironmentFile -}}EnvironmentFile={{ . }}
{{ end -}}
{{ if .Service.WorkingDirectory -}}WorkingDirectory={{ .Service.WorkingDirectory }}
{{ end -}}
{{ if .Service.MemoryMax -}}MemoryMax={{ .Service.MemoryMax }}
{{ end -}}
{{ if .Service.MemoryHigh -}}MemoryHigh={{ .Service.MemoryHigh }}
{{ end -}}
{{ if .Service.MemoryLow -}}MemoryLow={{ .Service.MemoryLow }}
{{ end -}}
{{ if .Service.MemoryMin -}}MemoryMin={{ .Service.MemoryMin }}
{{ end -}}
{{ if .Service.CPUQuota -}}CPUQuota={{ .Service.CPUQuota }}
{{ end -}}
{{ if .Service.CPUWeight -}}CPUWeight={{ .Service.CPUWeight }}
{{ end -}}
{{ if isset .Service "IOWeight" -}}IOWeight={{ printf "%d" .Service.IOWeight }}
{{ end -}}
{{ if .Service.TasksMax -}}TasksMax={{ .Service.TasksMax }}
{{ end -}}
{{ if .Service.LimitNOFILE -}}LimitNOFILE={{ .Service.LimitNOFILE }}
{{ end -}}
{{ if .Service.LimitNPROC -}}LimitNPROC={{ .Service.LimitNPROC }}
{{ end -}}
{{ if .Service.LimitCORE -}}LimitCORE={{ .Service.LimitCORE }}
{{ end -}}
{{ if .Service.LimitMEMLOCK -}}LimitMEMLOCK={{ .Service.LimitMEMLOCK }}
{{ end -}}
{{ end -}}
{{ if .Install }}
[Install]
{{ range .Install.WantedBy -}}WantedBy={{ . }}
{{ end -}}
{{ range .Install.RequiredBy -}}RequiredBy={{ . }}
{{ end -}}
{{ range .Install.UpheldBy -}}UpheldBy={{ . }}
{{ end -}}
{{ range .Install.Alias -}}Alias={{ . }}
{{ end -}}
{{ if .Install.DefaultInstance -}}DefaultInstance={{ .Install.DefaultInstance }}
{{ end -}}
{{ end -}}
