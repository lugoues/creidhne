package example

import "github.com/lugoues/creidhne"

secrets: creidhne.#SecretRegistry & {
	porkbun_api_key:     _
	porkbun_secret_api_key: _
  traefik_otel_auth: _
}
