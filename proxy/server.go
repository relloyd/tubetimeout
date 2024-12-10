package proxy

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"example.com/youtube-nfqueue/group"
	"example.com/youtube-nfqueue/models"
	"example.com/youtube-nfqueue/usage"
	"github.com/elazarl/goproxy"
)

func NewServer(m *group.Manager, t *usage.Tracker) *http.Server {
	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = true
	proxy.OnRequest().DoFunc(GetHandler(m, t))

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

// GetHandler returns a handle func that can allow/deny requests.
// The returned func will return a req,nil if the request is allowed, or nil,res if the request should be denied.
func GetHandler(m *group.Manager, t *usage.Tracker) func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	return func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		log.Println("Proxying request", req.RemoteAddr, req.URL)

		// Get the host from the request URL.
		destDomain := req.URL.Host
		if i := strings.Index(req.URL.Host, ":"); i != -1 { // if there is a port...
			destDomain = req.URL.Host[:i] // trim the port.
		}

		// Get the source from the request.
		srcIP := ctx.Req.RemoteAddr
		if i := strings.Index(ctx.Req.RemoteAddr, ":"); i != -1 { // if there is a port...
			srcIP = ctx.Req.RemoteAddr[:i] // trim the port.
		}

		// Use the groups associated with the source IP and destination domain.
		if g, ok := m.IsSrcIpDestDomainKnown(models.Ip(srcIP), models.Domain(destDomain)); ok {
			log.Printf("Proxying request to %v within group(s) %v", destDomain, g)
			count := 0
			for _, v := range g {
				t.AddSample(string(v))
				if t.HasExceededThreshold(string(v)) {
					// TODO: handle multiple groups in the proxy blocker.
					log.Printf("Proxy request from %v to %v denied. Threshold exceeded for group %v", srcIP, destDomain, g)
					return nil, createBlockedResponse(`Request exploded 💣💥`)
				}
				log.Printf("Proxy request from %v to %v granted", srcIP, destDomain)
				count++
			}
			if count == 0 {
				log.Printf("Proxy filter found no known groups for request from %v to %v", srcIP, destDomain)
			}
		}

		return req, nil
	}
}

func createBlockedResponse(reason string) *http.Response {
	body := bytes.NewBufferString(reason)
	return &http.Response{
		Status:        http.StatusText(http.StatusForbidden), // "403 Forbidden"
		StatusCode:    http.StatusForbidden,
		Header:        http.Header{"Content-Type": []string{"text/plain"}},
		Body:          io.NopCloser(body),
		ContentLength: int64(body.Len()),
	}
}
