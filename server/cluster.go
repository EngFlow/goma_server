// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package server

import (
	"context"
	"os"
	"path"
	"strings"
	"sync"

	gce "cloud.google.com/go/compute/metadata"

	"go.chromium.org/goma/server/log"
)

var (
	clusterOnce     sync.Once
	clusterName     string
	clusterLocation string
)

func initClusterName(ctx context.Context) {
	if !gce.OnGCE() {
		return
	}
	logger := log.FromContext(ctx)
	logger.Debug("trying to get cluster name from metadata")
	var err error
	clusterName, err = gce.InstanceAttributeValue("cluster-name")
	if err != nil {
		logger.Errorf("failed to get cluster-name on gce: %v", err)
	} else {
		clusterName = strings.TrimSpace(clusterName)
		logger.Infof("cluster name: %s", clusterName)
	}
	clusterLocation, err = gce.InstanceAttributeValue("cluster-location")
	if err != nil {
		logger.Errorf("failed to get cluster-location on gce: %v", err)
	} else {
		clusterLocation = strings.TrimSpace(clusterLocation)
		logger.Infof("cluster location: %s", clusterLocation)
	}
}

// ClusterName returns cluster name where server is running on.
func ClusterName(ctx context.Context) string {
	clusterOnce.Do(func() {
		initClusterName(ctx)
	})
	return clusterName
}

var (
	projectOnce sync.Once
	projectID   string
)

// ProjectID returns current project id.
func ProjectID(ctx context.Context) string {
	projectOnce.Do(func() {
		if !gce.OnGCE() {
			return
		}
		logger := log.FromContext(ctx)
		var err error
		projectID, err = gce.ProjectID()
		if err != nil {
			logger.Errorf("failed to get project id: %v", err)
		}
	})
	return projectID
}

// serverName returns service name, prefixed by cluster name if any.
func serverName(ctx context.Context, name string) string {
	clusterName := ClusterName(ctx)
	if clusterName != "" {
		name = path.Join(clusterName, name)
	}
	return name
}

var (
	hostnameOnce sync.Once
	hostname     string
)

func initHostname(ctx context.Context) {
	logger := log.FromContext(ctx)

	var err error
	hostname, err = os.Hostname()
	if err != nil {
		logger.Errorf("hostname: %v", err)
		hostname = os.Getenv("HOSTNAME")
	}
	logger.Infof("hostname: %s", hostname)
}

// HostName returns hostname. in k8s, it is podname.
func HostName(ctx context.Context) string {
	hostnameOnce.Do(func() {
		initHostname(ctx)
	})
	return hostname
}
