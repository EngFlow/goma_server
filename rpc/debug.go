// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package rpc

import (
	"context"
	"html/template"
	"net/http"
	"sort"
	"time"

	"go.chromium.org/goma/server/log"
)

func init() {
	http.HandleFunc("/debug/backends", func(w http.ResponseWriter, req *http.Request) {
		// TODO: auth request as net/trace
		w.Header().Set("Content-Type", "text/html; charset=utf=8")
		Render(w, req)
	})
}

// Render renders backend information.
// It is handled on /debug/backends on default http mux.
func Render(w http.ResponseWriter, req *http.Request) {
	type backendInfo struct {
		Addr string
		Conn bool
		NReq int64
		NErr int64
		Load int
		Err  error
	}
	type clientInfo struct {
		Target    string
		Timestamp time.Time
		Backends  []backendInfo
	}

	data := struct {
		Clients []clientInfo
	}{}

	clientsMu.Lock()
	for _, client := range clients {
		client.mu.RLock()
		ci := clientInfo{
			Target:    client.target,
			Timestamp: client.timestamp,
		}
		var addrs []string
		for addr := range client.backends {
			addrs = append(addrs, addr)
		}
		sort.Strings(addrs)
		for _, addr := range addrs {
			b := client.backends[addr]
			b.mu.Lock()
			bi := backendInfo{
				Addr: b.addr,
				Conn: b.cc != nil,
				NReq: b.nreq,
				NErr: b.nerr,
				Load: b.load,
				Err:  b.err,
			}
			ci.Backends = append(ci.Backends, bi)
			b.mu.Unlock()
		}
		client.mu.RUnlock()
		data.Clients = append(data.Clients, ci)
	}
	clientsMu.Unlock()

	err := pageTmpl.ExecuteTemplate(w, "Page", data)
	if err != nil {
		logger := log.FromContext(context.Background())
		logger.Errorf("rpc: Failed executing template: %v", err)
	}
}

var pageTmpl = template.Must(template.New("Page").Parse(pageHTML))

const pageHTML = `
{{template "Prolog" .}}
{{template "StatusTable" .}}
{{template "Epilog" .}}

{{define "Prolog"}}
<html>
 <head>
  <title>/debug/backends</title>
 </head>
 <body>
<h1>/debug/backends</h1>
{{end}}

{{define "StatusTable"}}
<table border="1">
 <tr>
  <th>target
  <td>timestamp
  <td>backends
  <td>conn
  <td># of reqs
  <td># of err
  <td>load
  <td>err
 {{range $c := .Clients}}
 <tr>
  <td>{{$c.Target}}
  <td>{{$c.Timestamp}}
  {{range $b := $c.Backends}}
  <tr>
   <td colspan="2">
   <td>{{$b.Addr}}
   <td>{{$b.Conn}}
   <td>{{$b.NReq}}
   <td>{{$b.NErr}}
   <td>{{$b.Load}}
   <td>{{$b.Err}}
  {{end}}
 {{end}}
</table>
{{end}}

{{define "Epilog"}}
 </body>
</html>
{{end}}
`
