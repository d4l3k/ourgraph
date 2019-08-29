package db

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"os"

	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var dgraphAddr = flag.String("dgraphaddr", "ourgraph-db.fn.lc:9080", "address of the dgraph instance")

func NewConn() (*grpc.ClientConn, error) {
	opts := []grpc.DialOption{
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(100 * 1000 * 1000)),
	}
	crt := os.Getenv("DGRAPH_CRT")
	key := os.Getenv("DGRAPH_KEY")
	ca := os.Getenv("DGRAPH_CA")
	if len(crt) > 0 {
		cert, err := tls.X509KeyPair([]byte(crt), []byte(key))
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(ca)) {
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

	return grpc.Dial(*dgraphAddr, opts...)
}
