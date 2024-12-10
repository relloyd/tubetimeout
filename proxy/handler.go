package proxy

import (
	"log"
	"net/http"
	"strings"
	"time"

	"example.com/youtube-nfqueue/group"
	"example.com/youtube-nfqueue/models"
	"github.com/elazarl/goproxy"
)

func NewServer(m *group.Manager) *http.Server {
	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = true
	proxy.OnRequest().DoFunc(GetHandler(m))

	return &http.Server{
		Addr:                         ":8080",
		Handler:                      proxy,
		DisableGeneralOptionsHandler: false,
		TLSConfig:                    nil,
		ReadTimeout:                  30 * time.Second, // Maximum duration for reading the request body
		ReadHeaderTimeout:            5 * time.Second,  // Time to read headers before timing out
		WriteTimeout:                 30 * time.Second, // Maximum duration for writing the response
		IdleTimeout:                  30 * time.Second, // Maximum amount of time to keep idle connections alive
		MaxHeaderBytes:               1 << 20,          // Maximum size of request headers (1 MB)
	}
}

func GetHandler(m *group.Manager) func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	return func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		log.Println("Proxying request", req.RemoteAddr, req.URL)

		i := strings.Index(req.URL.Host, ":")
		host := req.URL.Host[:i]
		if g, ok := m.IsDstDomainGroupKnown(models.Domain(host)); ok {
			log.Printf("Proxying request to %v within group %v", host, g)
			// TODO: track usage and allow/deny
		}

		return req, nil
	}
}
