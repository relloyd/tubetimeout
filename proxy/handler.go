package proxy

import (
	"log"
	"net/http"

	"github.com/elazarl/goproxy"
)

func NewProxy() *goproxy.ProxyHttpServer {
	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = true
	proxy.OnRequest().DoFunc(Handler)
	return proxy
}

func Handler(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	log.Println("Proxying request", req.RemoteAddr, req.URL)
	return req, nil
}
