package proxy

import (
	"net/http"

	"github.com/elazarl/goproxy"
)

func Handler(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {

	return req, nil
}
