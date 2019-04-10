// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

/*
Binary goma_grpc_client is a simple gRPC client of goma api.

 $ goma_grpc_client [-api_key <key>] <addr> <service>.<method> <request>

*/
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang/protobuf/proto"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"go.chromium.org/goma/server/auth/account"
	"go.chromium.org/goma/server/auth/enduser"
	"go.chromium.org/goma/server/log"
	gomapb "go.chromium.org/goma/server/proto/api"
	execpb "go.chromium.org/goma/server/proto/exec"
	execlogpb "go.chromium.org/goma/server/proto/execlog"
	filepb "go.chromium.org/goma/server/proto/file"
)

var (
	// https://cloud.google.com/endpoints/docs/grpc/restricting-api-access-with-api-keys-grpc
	apiKey = flag.String("api_key", "", "api_key")

	enduserEmail  = flag.String("enduser_email", "", "enduser email")
	enduserGroup  = flag.String("enduser_group", "", "enduser group")
	enduserSAJSON = flag.String("enduser_service_account_json", "", "enduser service account json file")

	tlsVerify = flag.Bool("tls_verify", true, "verifies the server's certificate chain and hostname.")
	insecure  = flag.Bool("insecure", false, "insecure connection, i.e. no TLS")
	verbose   = flag.Bool("v", false, "verbose flag")
)

type desc struct {
	method func(context.Context, proto.Message) (proto.Message, error)
	req    proto.Message
}

func protoDesc(ctx context.Context, conn *grpc.ClientConn, servMethod string) (desc, error) {
	// TODO: use proto descriptor?
	switch servMethod {
	case "devtools_goma.ExecService.Exec":
		client := execpb.NewExecServiceClient(conn)
		return desc{
			method: func(ctx context.Context, req proto.Message) (proto.Message, error) {
				return client.Exec(ctx, req.(*gomapb.ExecReq))
			},
			req: &gomapb.ExecReq{},
		}, nil

	case "devtools_goma.FileService.StoreFile":
		client := filepb.NewFileServiceClient(conn)
		return desc{
			method: func(ctx context.Context, req proto.Message) (proto.Message, error) {
				return client.StoreFile(ctx, req.(*gomapb.StoreFileReq))
			},
			req: &gomapb.StoreFileReq{},
		}, nil

	case "devtools_goma.FileService.LookupFile":
		client := filepb.NewFileServiceClient(conn)
		return desc{
			method: func(ctx context.Context, req proto.Message) (proto.Message, error) {
				return client.LookupFile(ctx, req.(*gomapb.LookupFileReq))
			},
			req: &gomapb.LookupFileReq{},
		}, nil

	case "devtools_goma.LogService.SaveLog":
		client := execlogpb.NewLogServiceClient(conn)
		return desc{
			method: func(ctx context.Context, req proto.Message) (proto.Message, error) {
				return client.SaveLog(ctx, req.(*gomapb.SaveLogReq))
			},
			req: &gomapb.SaveLogReq{},
		}, nil
	default:
		return desc{}, fmt.Errorf("unknown service: %s", servMethod)
	}
}

func setEnduser(ctx context.Context) context.Context {
	if *enduserEmail == "" || *enduserGroup == "" || *enduserSAJSON == "" {
		return ctx
	}
	logger := log.FromContext(ctx)
	p := account.JSONDir{
		Dir: filepath.Dir(*enduserSAJSON),
		Scopes: []string{
			"https://www.googleapis.com/auth/cloud-build-service",
		},
	}
	name := filepath.Base(*enduserSAJSON)
	name = strings.TrimRight(name, ".json")
	a, err := p.New(name)
	if err != nil {
		logger.Errorf("service account %s: %v", name, err)
		return ctx
	}
	token, err := a.Token(ctx)
	if err != nil {
		logger.Errorf("token %s: %v", name, err)
		return ctx
	}
	logger.Infof("enduser email=%q group=%q token=%v", *enduserEmail, *enduserGroup, token)
	return enduser.NewContext(ctx, enduser.New(*enduserEmail, *enduserGroup, token))
}

func main() {
	flag.Usage = func() {
		w := flag.CommandLine.Output()
		fmt.Fprintf(w, "Usage of %s:\n", os.Args[0])
		fmt.Fprintf(w, "%s <address> <service>.<method> <request>\n", os.Args[0])
		fmt.Fprintf(w, " <address>; host:port\n")
		fmt.Fprintf(w, " <service>; exported service name\n")
		fmt.Fprintf(w, "          ; e.g. devtools_goma.ExecService\n")
		fmt.Fprintf(w, " <method> ; method name. e.g Exec\n")
		fmt.Fprintf(w, " <request>; text protobuffer for request\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	ctx := context.Background()

	if !*verbose {
		log.SetZapLogger(zap.NewNop())
	}
	logger := log.FromContext(ctx)
	fatalf := func(format string, args ...interface{}) {
		if !*verbose {
			// logger.Fatalf won't print if we set zap.NewNop...
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		}
		logger.Fatalf(format, args...)
	}
	if flag.NArg() < 3 {
		flag.Usage()
		os.Exit(2)
	}
	address := flag.Arg(0)
	servMethod := flag.Arg(1)
	requestMsg := flag.Arg(2)

	certPool, err := x509.SystemCertPool()
	if err != nil {
		fatalf("system cert pool: %v", err)
	}
	if certPath := os.Getenv("GOMA_SSL_EXTRA_CERT"); certPath != "" {
		buf, err := ioutil.ReadFile(certPath)
		if err != nil {
			fatalf("cert file %q: %v", certPath, err)
		}
		ok := certPool.AppendCertsFromPEM(buf)
		if !ok {
			fatalf("set cert from $GOMA_SSL_EXTRA_CERT")
		}
	}
	if certData := os.Getenv("GOMA_SSL_EXTRA_CERT_DATA"); certData != "" {
		logger.Infof("using GOMA_SSL_EXTRA_CERT_DATA:\n%s", certData)
		ok := certPool.AppendCertsFromPEM([]byte(certData))
		if !ok {
			fatalf("set cert from $GOMA_SSL_EXTRA_CERT_DATA")
		}
	}
	insecureSkipVerify := !*tlsVerify
	if insecureSkipVerify {
		logger.Warnf("insecure skip verify. accepts any certificate")
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			RootCAs:            certPool,
			InsecureSkipVerify: insecureSkipVerify,
		})),
	}
	if *insecure {
		logger.Warnf("insecure connection")
		opts[0] = grpc.WithInsecure()
	}
	conn, err := grpc.DialContext(ctx, address, opts...)
	if err != nil {
		fatalf("dial: %v", err)
	}
	defer conn.Close()

	desc, err := protoDesc(ctx, conn, servMethod)
	if err != nil {
		fatalf("desc: %v", err)
	}
	err = proto.UnmarshalText(requestMsg, desc.req)
	if err != nil {
		fatalf("request: %v", err)
	}

	if *apiKey != "" {
		logger.Infof("Using api_key: %s", *apiKey)
		ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("x-api-key", *apiKey))
	}
	ctx = setEnduser(ctx)
	md, _ := metadata.FromOutgoingContext(ctx)
	logger.Debugf("outgoing metatada: %v", md)

	resp, err := desc.method(ctx, desc.req)
	if err != nil {
		fatalf("call: %v", err)
	}
	fmt.Println("response:\n", proto.MarshalTextString(resp))
}
