package core

import (
	"clonarr/internal/arr"
	"net/http"
)

// NewArrClientFor constructs an ArrClient for the given instance, including
// HTTP Basic Auth credentials when ExternalAuth is enabled on the instance.
func NewArrClientFor(inst Instance, client *http.Client) *arr.ArrClient {
	u, p := "", ""
	if inst.ExternalAuth {
		u, p = inst.Username, inst.Password
	}
	return arr.NewArrClient(inst.URL, inst.APIKey, u, p, client)
}
