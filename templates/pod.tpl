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
{{ template "install" .Install }}