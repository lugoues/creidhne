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
{{ range .Build.Network -}}Network={{ . }}
{{ end -}}
{{ range .Build.DNS -}}DNS={{ . }}
{{ end -}}
{{ range .Build.DNSOption -}}DNSOption={{ . }}
{{ end -}}
{{ range .Build.DNSSearch -}}DNSSearch={{ . }}
{{ end -}}
{{ range .Build.Label -}}Label={{ . }}
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
{{ range .Build.Volume -}}Volume={{ . }}
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