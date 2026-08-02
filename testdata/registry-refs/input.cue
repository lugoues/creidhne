@extern(embed)

package registry_refs

import (
	"github.com/lugoues/creidhne"
	"github.com/lugoues/quadlets-test:testing"
)

// All three registries consumed through their handles: #ImageRegistry.#ref
// (pinned -> image@digest, unpinned -> bare tag), #SecretRegistry entries
// unified with consumption fields into Secret=, and #AssetRegistry.#ref
// expanded into build-context entries. This is the CUE<->Go registry contract,
// byte-locked.
images: creidhne.#ImageRegistry & {
	app: {
		image:  "ghcr.io/acme/app:2.3.0"
		digest: "sha256:6339c0ffee00000000000000000000000000000000000000000000000000beef"
	}
	tracker: image: "docker.io/library/redis:7.2"
}

secrets: creidhne.#SecretRegistry & {
	db_password: _
	tls: {name: "tls-cert"}
	session: generate: {length: 32}
}

assets: creidhne.#AssetRegistry & {
	conf: source: "assets/*.ini"
}

test: testing.#Test & {
	subject: creidhne.#Quadlet & {
		name: "app"
		units: {
			#build: {
				ContainerFile: """
					FROM docker.io/library/alpine
					COPY etc /etc/app
					"""
				Context: etc: assets.conf.#ref
			}
			#container: Container: {
				Image:         images.app.#ref
				ContainerName: "app"
				Secret: [
					// Direct unification: the pre-registry pattern, valid for
					// metadata-free entries.
					secrets.db_password & {type: "env", target: "DB_PASSWORD"},
					secrets.tls & {type: "mount", target: "/etc/ssl/cert.pem", mode: "0400"},
					// The #ref handle: the form that also works when the entry
					// carries management metadata (generate).
					secrets.session.#ref & {type: "env", target: "SESSION_KEY"},
				]
			}
			containers: tracker: Container: Image: images.tracker.#ref
		}
	}
	expected: {
		"app.build":     _ @embed(file=expected/app.build,type=text)
		"app.container": _ @embed(file=expected/app.container,type=text)
		"app-tracker.container": _ @embed(file=expected/app-tracker.container,type=text)
		"images/app.Containerfile": _ @embed(file=expected/images/app.Containerfile,type=text)
		"images/app.context/etc/app.ini": _ @embed(file=expected/images/app.context/etc/app.ini,type=text)
	}
}
