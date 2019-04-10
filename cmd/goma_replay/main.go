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
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"go.chromium.org/goma/server/httprpc"
	"go.chromium.org/goma/server/log"

	gomapb "go.chromium.org/goma/server/proto/api"
)

var (
	dataSourceDir = flag.String("data_source_dir", gomaTmpDir(), "data source directory")

	repeat = flag.Int("n", 1, "N repeat request")
	limit  = flag.Int("l", 1, "limit at most N request")

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
		reqs = append(reqs, req)
		fmt.Println("use ", path)
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
	resp, err := http.Get(fmt.Sprintf("https://oauth2.googleapis.com/tokeninfo?access_token=%s", token.AccessToken))
	if err != nil {
		return nil, "", fmt.Errorf("%s: tokeninfo %v", fname, err)
	}
	defer resp.Body.Close()
	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("%s: tokeninfo body %v", fname, err)
	}
	var tokeninfo struct {
		Email string `json:"email"`
	}
	err = json.Unmarshal(b, &tokeninfo)
	if err != nil {
		return nil, "", fmt.Errorf("%s: tokeninfo json %v", fname, err)
	}
	return c.Client(ctx, token), tokeninfo.Email, nil
}

func newClient(ctx context.Context) (*http.Client, string, error) {
	logger := log.FromContext(ctx)
	paths := []string{os.Getenv("GOMA_OAUTH2_CONFIG_FILE")}
	if *asInternal {
		paths = append(paths, os.ExpandEnv("$HOME/.goma_oauth2_config"))
	} else {
		paths = append(paths, os.ExpandEnv("$HOME/.goma_client_oauth2_config"))
	}
	for _, p := range paths {
		logger.Infof("oauth2 config: %s", p)
		c, email, err := oauth2Client(ctx, p)
		if err != nil {
			logger.Warnf("oauth2 config %s: %v", p, err)
			continue
		}
		return c, email, nil
	}
	return nil, "", fmt.Errorf("no goma oauth2 config avaialble: %s", paths)
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
	reqs, err := loadRequestData(ctx, *dataSourceDir)
	if err != nil {
		fatalf("request data: %v", err)
	}

	t0 := time.Now()
	var wg sync.WaitGroup
	sema := make(chan bool, *limit)
	for i, req := range reqs {
		for j := 0; j < *repeat; j++ {
			wg.Add(1)
			go func(i int, req *gomapb.ExecReq) {
				defer wg.Done()
				sema <- true
				defer func() {
					<-sema
				}()
				resp := &gomapb.ExecResp{}
				t := time.Now()
				err = httprpc.Call(ctx, c, targ.String(), req, resp)
				if err != nil {
					errorf("req[%d] %v", i, err)
				}
				if e := resp.GetError(); e != gomapb.ExecResp_OK {
					errorf("req[%d] ExecError %s", e)
					logger.Debugf("%s", resp)
				}

				logger.Infof("req[%d] %s %s", i, resp.GetError(), time.Since(t))
			}(i, req)
		}
	}
	wg.Wait()
	fmt.Printf("%s %d reqs * %d (limit:%d) in %s\n", targ, len(reqs), *repeat, *limit, time.Since(t0))
}
