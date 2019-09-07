package db

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"log"
	"os"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var dgraphAddr = flag.String("dgraphaddr", "ourgraph-db.fn.lc:9080", "address of the dgraph instance")

const CA = `-----BEGIN CERTIFICATE-----
MIIDNDCCAhygAwIBAgIIckKrr7Q0fT8wDQYJKoZIhvcNAQELBQAwRjEaMBgGA1UE
ChMRRGdyYXBoIExhYnMsIEluYy4xFzAVBgNVBAMTDkRncmFwaCBSb290IENBMQ8w
DQYDVQQFEwY3MjQyYWIwHhcNMTkwODIyMDM1ODMxWhcNMjkwODIxMDM1ODMxWjBG
MRowGAYDVQQKExFEZ3JhcGggTGFicywgSW5jLjEXMBUGA1UEAxMORGdyYXBoIFJv
b3QgQ0ExDzANBgNVBAUTBjcyNDJhYjCCASIwDQYJKoZIhvcNAQEBBQADggEPADCC
AQoCggEBANu3jXmET5D0dHTa/OZHxxKzg203GqZ7lETXudZdTuZdJgl9yTRerDu6
YnfbDIC4fiAya5kYtb1zoRtsrQIRBa0y4jbHSBOzsO0P3SW9jqPAMz+I+iYtwGnC
fc3pHOdTRnPcpFLkU2lr/3Im9/SusJAeKO1hnPAV+BPfLcmTvgFahoerYcgXzCCN
X2ajSZ24wujlGWJryWd3DdJ++rXInGv9vBH3EAxYz8k2DWvoNo2wxFiHvoP9YA+s
ueP+DoyLvrS/PhwI0geab+XVyBj+uDX0MXA2CGiG3Q9VwpLmawHqIHXt4dfag39i
xSD3kMAkfnnU1MvWbfoTKXI3cJe+p2cCAwEAAaMmMCQwDgYDVR0PAQH/BAQDAgLk
MBIGA1UdEwEB/wQIMAYBAf8CAQAwDQYJKoZIhvcNAQELBQADggEBAGeIwU/4OvME
7T1LZIf4vFj6V4DtsDrZknviEFadGVNAqACcluZlxsWdM7yKqFyrQX4EyzrGbfQH
hRqinNGMTGk9T0Ps4exN9qETM+TDnL4T+dPawmlIekv1cfrRfZpRYe7hc4Lk37j5
xy3aMbfsIvT4YsuDXUJRI6UfPjZsQ6QSCNLdb4cSDYee9guSXgEUeO2JDyxODYnd
2my2Y0Mskh5Y8phnFJRUxCF5VPvnG9UdaAJlrCKNO3rcZjgvs8OWfJv2QxzW/PRq
5NuztvKRYc7CHdAKl4oJzELrXiQ68tuC/QdfIJrvocObjXgcoBAtbr3wZ/DMmJ2y
yQCcxjPCUH8=
-----END CERTIFICATE-----`

func NewConn(ctx context.Context) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(ctx, 1*time.Minute)
	defer cancel()
	opts := []grpc.DialOption{
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(100 * 1000 * 1000)),
		grpc.WithBlock(),
	}
	crt := os.Getenv("DGRAPH_CRT")
	key := os.Getenv("DGRAPH_KEY")
	if len(crt) > 0 {
		log.Printf("using SSL")
		cert, err := tls.X509KeyPair([]byte(crt), []byte(key))
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(CA)) {
			return nil, errors.Errorf("invalid CA cert")
		}
		opts = append(
			opts,
			grpc.WithTransportCredentials(
				credentials.NewTLS(
					&tls.Config{
						Certificates: []tls.Certificate{cert},
						ServerName:   "node",
						RootCAs:      pool,
					},
				),
			),
		)
	} else {
		opts = append(opts, grpc.WithInsecure())
	}

	addr := *dgraphAddr
	log.Printf("connecting to %q...", addr)
	return grpc.DialContext(ctx, addr, opts...)
}
