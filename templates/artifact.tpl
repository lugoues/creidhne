{{ template "unit" .Unit }}
[Artifact]
Artifact={{ .Artifact.Artifact }}
{{ if .Artifact.ServiceName -}}ServiceName={{ .Artifact.ServiceName }}
{{ end -}}
{{ if .Artifact.AuthFile -}}AuthFile={{ .Artifact.AuthFile }}
{{ end -}}
{{ if .Artifact.CertDir -}}CertDir={{ .Artifact.CertDir }}
{{ end -}}
{{ if .Artifact.Creds -}}Creds={{ .Artifact.Creds }}
{{ end -}}
{{ if .Artifact.DecryptionKey -}}DecryptionKey={{ .Artifact.DecryptionKey }}
{{ end -}}
{{ if .Artifact.TLSVerify -}}TLSVerify=true
{{ end -}}
{{ if .Artifact.Quiet -}}Quiet=true
{{ end -}}
{{ if isset .Artifact "Retry" -}}Retry={{ printf "%d" .Artifact.Retry }}
{{ end -}}
{{ if .Artifact.RetryDelay -}}RetryDelay={{ .Artifact.RetryDelay }}
{{ end -}}
{{ range .Artifact.GlobalArgs -}}GlobalArgs={{ . }}
{{ end -}}
{{ range .Artifact.PodmanArgs -}}PodmanArgs={{ . }}
{{ end -}}
{{ range .Artifact.ContainersConfModule -}}ContainersConfModule={{ . }}
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