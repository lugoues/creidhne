@extern(embed)

package unit_install_full

import (
	"github.com/lugoues/quadlets"
	"github.com/lugoues/quadlets-test:testing"
)

_mode:     *"test" | "update" @tag(mode)
_expected: _ @embed(file=expected.quadlets,type=text)

test: testing.#Test & {
	subject: quadlets.#Quadlet & {
		name: "fullunit"
		units: {
			#container: {
				Unit: {
					Description:          "Full unit test container"
					Documentation:        "https://example.com/docs"
					After: ["network-online.target", "redis.service"]
					Before: ["app-ready.target"]
					Requires: ["redis.service"]
					Wants: ["network-online.target"]
					BindsTo: ["redis.service"]
					PartOf: ["app.target"]
					Conflicts: ["legacy-app.service"]
					Condition: ["ConditionPathExists=/etc/app.conf"]
					Assert: ["AssertPathExists=/opt/app/bin"]
					SourcePath:            "/etc/systemd/system/fullunit.container"
					StopWhenUnneeded:      true
					RefuseManualStart:     true
					RefuseManualStop:      true
					AllowIsolate:          true
					IgnoreOnIsolate:       true
					OnSuccess:             "notify-success.service"
					OnFailure:             "notify-failure.service"
					OnSuccessJobMode:      "fail"
					OnFailureJobMode:      "replace"
					StartLimitIntervalSec: "60s"
					StartLimitBurst:       5
				}
				Container: {
					Image: "docker.io/fullunit:latest"
				}
				Install: {
					WantedBy: ["multi-user.target"]
					RequiredBy: ["app.target"]
					UpheldBy: ["core.target"]
					Alias: ["myapp.service"]
					DefaultInstance: "default"
				}
			}
		}
	}
	if _mode == "test" {
		expected: _expected
	}
}
