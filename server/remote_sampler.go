// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package server

import "go.opencensus.io/trace"

type remoteSampler struct {
	sampler trace.Sampler
}

func (rs remoteSampler) Sample(p trace.SamplingParameters) trace.SamplingDecision {
	if p.HasRemoteParent {
		return trace.SamplingDecision{
			Sample: true,
		}
	}
	return rs.sampler(p)
}

// NewRemoteSampler returns trace sampler to sample if remote is sampled
// if remoteSampled is true.
// if remoteSampled is false or remote is not sampled, use sampler.
func NewRemoteSampler(remoteSampled bool, sampler trace.Sampler) trace.Sampler {
	if remoteSampled {
		return remoteSampler{
			sampler: sampler,
		}.Sample
	}
	return sampler
}
