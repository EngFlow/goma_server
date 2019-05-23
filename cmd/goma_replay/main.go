// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

/*
Binary goma_replay is a simple goma client for load testing etc.

"dump exec_req" on goma client, and run

 $ goma_replay -n 100 -l 100

*/
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/golang/protobuf/proto"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	googleoauth2 "google.golang.org/api/oauth2/v2"

	"go.chromium.org/goma/server/httprpc"
	"go.chromium.org/goma/server/log"

	gomapb "go.chromium.org/goma/server/proto/api"
)

var (
	dataSourceDir = flag.String("data_source_dir", gomaTmpDir(), "data source directory")

	repeat  = flag.Int("n", 1, "N repeat request")
	limit   = flag.Int("l", 1, "limit at most N request")
	buildID = flag.String("build_id", "", "overrides build_id")

	asInternal = flag.Bool("as_internal", true, "use oauth2 for internal client")
	verbose    = flag.Bool("v", false, "verbose flag")
)

func gomaTmpDir() string {
	// client/mypath.cc GetGomaTmpDir
	if v := os.Getenv("GOMA_TMP_DIR"); v != "" {
		return v
	}
	u, err := user.Current()
	if err != nil {
		panic(err)
	}
	return filepath.Join("/run/user", u.Uid, "goma_"+u.Username)
}

func clearInputs(req *gomapb.ExecReq) {
	for i := range req.Input {
		req.Input[i].Content = nil
	}
}

func loadRequestData(ctx context.Context, dir string) ([]*gomapb.ExecReq, error) {
	var reqs []*gomapb.ExecReq
	logger := log.FromContext(ctx)
	logger.Infof("loading from %s", dir)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		logger.Debugf("datasource: %s", path)
		if filepath.Base(path) != "exec_req.data" {
			return nil
		}
		b, err := ioutil.ReadFile(path)
		if err != nil {
			return err
		}
		req := &gomapb.ExecReq{}
		err = proto.Unmarshal(b, req)
		if err != nil {
			return fmt.Errorf("%s: %v", path, err)
		}
		clearInputs(req)
		if *buildID != "" {
			req.GetRequesterInfo().BuildId = proto.String(*buildID)
		}
		reqs = append(reqs, req)
		fmt.Printf("req[%d]=%s\n", len(reqs)-1, path)
		logger.Debugf("req[%d]=%s", len(reqs)-1, req)
		return nil
	})
	logger.Infof("loaded %d req(s): err=%v", len(reqs), err)
	return reqs, err
}

func oauth2Client(ctx context.Context, fname string) (*http.Client, string, error) {
	logger := log.FromContext(ctx)
	b, err := ioutil.ReadFile(fname)
	if err != nil {
		return nil, "", err
	}
	var config struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		RedirectURL  string `json:"redirect_uri"`
		Scope        string `json:"scope"`
		AuthURI      string `json:"auth_uri"`
		TokenURI     string `json:"token_uri"`
		RefreshToken string `json:"refresh_token"`
	}
	err = json.Unmarshal(b, &config)
	if err != nil {
		return nil, "", fmt.Errorf("oauth2 config %s: %v", fname, err)
	}
	c := &oauth2.Config{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  config.AuthURI,
			TokenURL: config.TokenURI,
		},
		RedirectURL: config.RedirectURL,
		Scopes:      []string{config.Scope},
	}
	t := &oauth2.Token{
		TokenType:    "Bearer",
		RefreshToken: config.RefreshToken,
	}
	token, err := c.TokenSource(ctx, t).Token()
	if err != nil {
		return nil, "", fmt.Errorf("oauth2 token %s: %v", fname, err)
	}
	logger.Debugf("access token: %s", token.AccessToken)

	s, err := googleoauth2.NewService(ctx)
	if err != nil {
		return nil, "", err
	}
	tokeninfo, err := s.Tokeninfo().Context(ctx).AccessToken(token.AccessToken).Do()
	if err != nil {
		return nil, "", err
	}
	return c.Client(ctx, token), tokeninfo.Email, nil
}

func oauth2ServiceAccountClient(ctx context.Context, fname string) (*http.Client, string, error) {
	b, err := ioutil.ReadFile(fname)
	if err != nil {
		return nil, "", err
	}
	c, err := google.JWTConfigFromJSON(b, googleoauth2.UserinfoEmailScope)
	if err != nil {
		return nil, "", err
	}
	return c.Client(ctx), c.Email, nil
}

func newClient(ctx context.Context) (*http.Client, string, error) {
	logger := log.FromContext(ctx)

	// client http_init.cc: InitHttpClientOptions
	for _, ca := range []struct {
		desc      string
		newClient func(context.Context) (*http.Client, string, error)
	}{
		// TODO: GOMA_HTTP_AUTHORIZATION_FILE
		{
			desc: "GOMA_OAUTH2_CONFIG_FILE",
			newClient: func(ctx context.Context) (*http.Client, string, error) {
				fname := os.Getenv("GOMA_OAUTH2_CONFIG_FILE")
				if fname == "" {
					return nil, "", errors.New("no GOMA_OAUTH2_CONFIG_FILE")
				}
				logger.Infof("oauth2 config: %s", fname)
				return oauth2Client(ctx, fname)
			},
		},
		{
			desc: "GOMA_SERVICE_ACCOUNT_JSON_FILE",
			newClient: func(ctx context.Context) (*http.Client, string, error) {
				fname := os.Getenv("GOMA_SERVICE_ACCOUNT_JSON_FILE")
				if fname == "" {
					return nil, "", errors.New("no GOMA_SERVICE_ACCOUNT_JSON_FILE")
				}
				logger.Infof("service account: %s", fname)
				return oauth2ServiceAccountClient(ctx, fname)
			},
		},
		// TODO: GOMA_USE_GCE_SERVICE_ACCOUNT
		// TODO: LUCI_CONTEXT
		{
			desc: "default goma oauth2 config file",
			newClient: func(ctx context.Context) (*http.Client, string, error) {
				fname := os.ExpandEnv("$HOME/.goma_client_oauth2_config")
				if *asInternal {
					fname = os.ExpandEnv("$HOME/.goma_oauth2_config")
				}
				logger.Infof("oauth2 config: %s", fname)
				return oauth2Client(ctx, fname)
			},
		},
	} {
		logger.Infof("client auth %s", ca.desc)
		c, email, err := ca.newClient(ctx)
		if err != nil {
			logger.Warnf("client auth %s: %v", ca.desc, err)
			continue
		}
		return c, email, nil
	}
	return nil, "", errors.New("no goma auth avaialble")
}

type target struct {
	host        string
	port        int
	useSSL      bool
	extraParams string
}

func (t target) String() string {
	scheme := "http"
	if t.useSSL {
		scheme = "https"
	}
	if (scheme == "http" && t.port == 80) ||
		(scheme == "https" && t.port == 443) {
		return fmt.Sprintf("%s://%s/cxx-compiler-service/e%s", scheme, t.host, t.extraParams)
	}
	return fmt.Sprintf("%s://%s:%d/cxx-compiler-service/e/%s", scheme, t.host, t.port, t.extraParams)
}

func newTargetFromEnv(ctx context.Context) (target, error) {
	u, err := user.Current()
	if err != nil {
		return target{}, err
	}
	t := target{
		host:   fmt.Sprintf("rbe-dev.endpoints.goma-%s.cloud.goog", u.Username),
		port:   443,
		useSSL: true,
	}
	if v := os.Getenv("GOMA_SERVER_HOST"); v != "" {
		t.host = v
	}
	if v := os.Getenv("GOMA_SERVER_PORT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return t, fmt.Errorf("GOMA_SERVER_PORT=%s; %v", v, err)
		}
		t.port = n
	}
	if v := os.Getenv("GOMA_USE_SSL"); v != "" {
		// client/env_flags.cc GOMA_EnvToBool
		t.useSSL = strings.ContainsAny(v, "tTyY1")
	}
	if v := os.Getenv("GOMA_RPC_EXTRA_PARAMS"); v != "" {
		t.extraParams = v
	}
	// proxy, url_path_prefix?
	return t, nil
}

type dialer struct {
	d     *net.Dialer
	mu    sync.Mutex
	addrs map[string]string
}

func (d *dialer) resolve(ctx context.Context, host string) (string, error) {
	if a, ok := d.addrs[host]; ok {
		return a, nil
	}
	logger := log.FromContext(ctx)
	addrs, err := net.LookupHost(host)
	if err != nil {
		logger.Warnf("resolve failed %q: %q", host, err)
		return "", err
	}
	logger.Infof("resolve %q => %q", host, addrs[0])
	d.mu.Lock()
	d.addrs[host] = addrs[0]
	d.mu.Unlock()
	return addrs[0], nil
}

func (d *dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addr, err := d.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	for i := 0; i < 5; i++ {
		conn, err := d.d.DialContext(ctx, network, net.JoinHostPort(addr, port))
		if err != nil {
			continue
		}
		return conn, nil
	}
	return nil, fmt.Errorf("too many retry to dial to %s:%s", network, address)
}

func main() {
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
	errorf := func(format string, args ...interface{}) {
		if !*verbose {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		}
		logger.Errorf(format, args...)
	}

	d := &dialer{
		d: &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		},
		addrs: make(map[string]string),
	}

	// https://golang.org/pkg/net/http/#RoundTripper
	http.DefaultTransport = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           d.DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	c, email, err := newClient(ctx)
	if err != nil {
		fatalf("client: %v", err)
	}
	fmt.Println("client:", email)
	targ, err := newTargetFromEnv(ctx)
	if err != nil {
		fatalf("target: %v", err)
	}
	fmt.Println("target:", targ)
	// Warm up the resolver cache
	_, err = d.resolve(ctx, targ.host)
	if err != nil {
		fatalf("resolver warm up: %v", err)
	}
	reqs, err := loadRequestData(ctx, *dataSourceDir)
	if err != nil {
		fatalf("request data: %v", err)
	}
	if *buildID != "" {
		fmt.Println("build_id:", *buildID)
	}

	sigch := make(chan os.Signal, 1)
	signal.Notify(sigch, syscall.SIGTERM, syscall.SIGINT)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		for {
			<-sigch
			fmt.Println("interrupted")
			if ctx.Err() != nil {
				fmt.Println("interrupted again. exiting...")
				os.Exit(1)
			}
			cancel()
		}
	}()

	t0 := time.Now()
	var wg sync.WaitGroup
	nreq, nrep := len(reqs), *repeat
	// atomic counters
	var (
		running, finished int32
		httpErrors        int32
		gomaExecErrors    int32
		gomaMissingInputs int32
		gomaOtherErrors   int32
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		sema := make(chan bool, *limit)
	Loop:
		for i, req := range reqs {
			for j := 0; j < *repeat; j++ {
				if ctx.Err() != nil {
					nreq = i
					nrep = j
					break Loop
				}
				wg.Add(1)
				sema <- true
				go func(i, j int, req *gomapb.ExecReq) {
					defer wg.Done()
					defer func() {
						atomic.AddInt32(&running, -1)
						atomic.AddInt32(&finished, 1)
						<-sema
					}()
					atomic.AddInt32(&running, 1)
					resp := &gomapb.ExecResp{}
					t := time.Now()
					err = httprpc.Call(ctx, c, targ.String(), req, resp)
					if err != nil {
						if ctx.Err() != nil {
							return
						}
						atomic.AddInt32(&httpErrors, 1)
						errorf("req[%d]%d %v", i, j, err)
					}
					if e := resp.GetError(); e != gomapb.ExecResp_OK {
						atomic.AddInt32(&gomaExecErrors, 1)
						errorf("req[%d]%d ExecError %s %s", i, j, e, time.Since(t))
						logger.Debugf("%s", resp)
					}
					if len(resp.MissingInput) > 0 {
						atomic.AddInt32(&gomaMissingInputs, 1)
						errorf("req[%d]%d missing inputs=%d %s", i, j, len(resp.MissingInput), time.Since(t))
					}
					if len(resp.ErrorMessage) > 0 {
						atomic.AddInt32(&gomaOtherErrors, 1)
						errorf("req[%d]%d error %q %s", i, j, resp.ErrorMessage, time.Since(t))
					}
					logger.Infof("req[%d]%d %s %s", i, j, resp.GetError(), time.Since(t))
				}(i, j, req)
				fmt.Printf("req[%d]%d/%d %d/%d error %d/%d/%d/%d %s\r", i, j, *repeat,
					atomic.LoadInt32(&running),
					atomic.LoadInt32(&finished),
					atomic.LoadInt32(&httpErrors),
					atomic.LoadInt32(&gomaExecErrors),
					atomic.LoadInt32(&gomaMissingInputs),
					atomic.LoadInt32(&gomaOtherErrors),
					time.Since(t0))
			}
		}
	}()
	wg.Wait()
	fmt.Printf("%s %d/%d reqs * %d/%d (limit:%d) finished=%d error http=%d/exec=%d/missing=%d/other=%d in %s\n", targ, nreq, len(reqs), nrep, *repeat, *limit,
		atomic.LoadInt32(&finished),
		atomic.LoadInt32(&httpErrors),
		atomic.LoadInt32(&gomaExecErrors),
		atomic.LoadInt32(&gomaMissingInputs),
		atomic.LoadInt32(&gomaOtherErrors),
		time.Since(t0))
}
