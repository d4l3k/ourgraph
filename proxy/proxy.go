package proxy

import (
	"flag"
	"log"
	"math/rand"
	"net"
	"net/http"
	"time"

	"go.uber.org/ratelimit"
	"golang.org/x/net/proxy"
)

var (
	socksAddr = flag.String("socksaddr", "proxy-nl.privateinternetaccess.com:1080", "address of socks server")
	socksUser = flag.String("socksuser", "", "socks5 username")
	socksPass = flag.String("sockspass", "", "socks5 password")
)

type client struct {
	limit  ratelimit.Limiter
	client http.Client
}

type Pool struct {
	clients []client
}

func MakePool(limiter func() ratelimit.Limiter) (*Pool, error) {
	host, port, err := net.SplitHostPort(*socksAddr)
	if err != nil {
		return nil, err
	}
	addrs, err := net.LookupHost(host)
	if err != nil {
		return nil, err
	}
	log.Printf("dns lookup for %q found: %+v", host, addrs)
	p := &Pool{
		clients: []client{
			{
				limit: limiter(),
				client: http.Client{
					Timeout: 1 * time.Minute,
				},
			},
		},
	}
	for _, addr := range addrs {
		dial, err := proxy.SOCKS5("tcp", net.JoinHostPort(addr, port), &proxy.Auth{User: *socksUser, Password: *socksPass}, nil)
		if err != nil {
			return nil, err
		}
		p.clients = append(p.clients, client{
			limit: limiter(),
			client: http.Client{
				Transport: &http.Transport{
					DialContext: dial.(proxy.ContextDialer).DialContext,
				},
				Timeout: 1 * time.Minute,
			},
		})
	}
	return p, nil
}

func (p *Pool) Get() *http.Client {
	client := &p.clients[rand.Intn(len(p.clients))]
	client.limit.Take()
	return &client.client
}
