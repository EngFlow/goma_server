// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package server

import (
	"sync"
	"time"

	"go.opencensus.io/trace"
)

type limitedSampler struct {
	sampler        trace.Sampler
	sampleDuration time.Duration

	mu          sync.Mutex
	lastSampled time.Time
}

func (ls *limitedSampler) Sample(p trace.SamplingParameters) trace.SamplingDecision {
	d := ls.sampler(p)
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if d.Sample && time.Since(ls.lastSampled) < ls.sampleDuration {
		d.Sample = false
	}
	if d.Sample {
		ls.lastSampled = time.Now()
	}
	return d
}

// NewLimitedSampler returns trace sampler limited by fraction and qps.
func NewLimitedSampler(fraction, qps float64) trace.Sampler {
	return (&limitedSampler{
		sampler:        trace.ProbabilitySampler(fraction),
		lastSampled:    time.Now(),
		sampleDuration: time.Duration(float64(1*time.Second) / qps),
	}).Sample
}
