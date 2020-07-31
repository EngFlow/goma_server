// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package rpc

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type retrySpy struct {
	errs []error
	n    int
}

func (r *retrySpy) f() error {
	i := r.n
	r.n++
	if i < len(r.errs) {
		return r.errs[i]
	}
	return errors.New("too many errors")
}

func TestRetry(t *testing.T) {
	origTimeAfter := timeAfter
	timeAfter = func(d time.Duration) <-chan time.Time {
		ch := make(chan time.Time)
		close(ch)
		return ch
	}
	defer func() {
		timeAfter = origTimeAfter
	}()
	for _, tc := range []struct {
		desc    string
		retry   Retry
		f       *retrySpy
		wantN   int
		wantErr bool
	}{
		{
			desc: "success",
			f: &retrySpy{
				errs: []error{nil},
			},
			wantN: 1,
		},
		{
			desc: "error",
			f: &retrySpy{
				errs: []error{errors.New("non retriable error")},
			},
			wantN:   1,
			wantErr: true,
		},
		{
			desc: "retry with retriable error",
			f: &retrySpy{
				errs: []error{
					RetriableError{
						Err: errors.New("retriable error"),
					},
					nil,
				},
			},
			wantN: 2,
		},
		{
			desc: "retry with unavailable",
			f: &retrySpy{
				errs: []error{
					status.Error(codes.Unavailable, "unavailable -> retry"),
					nil,
				},
			},
			wantN: 2,
		},
		{
			desc: "retry with internal transport unexpected content-type",
			f: &retrySpy{
				errs: []error{
					status.Error(codes.Internal, transportUnexpectedContentType),
					nil,
				},
			},
			wantN: 2,
		},
		{
			desc: "retry with internal transport unexpected EOF",
			f: &retrySpy{
				errs: []error{
					status.Errorf(codes.Internal, "call method: %v", io.ErrUnexpectedEOF),
					nil,
				},
			},
			wantN: 2,
		},
		{
			desc: "retry with stream terminated by RST STREAM with error code INTERNAL ERROR",
			f: &retrySpy{
				errs: []error{
					status.Errorf(codes.Internal, "call method: %v", streamTerminatedByRSTInternalError),
					nil,
				},
			},
			wantN: 2,
		},
		{
			desc: "no retry with internal error",
			f: &retrySpy{
				errs: []error{
					status.Error(codes.Internal, "non retriable internal error"),
					nil,
				},
			},
			wantN:   1,
			wantErr: true,
		},
		{
			desc: "no retry with bare unexpected EOF",
			f: &retrySpy{
				errs: []error{
					io.ErrUnexpectedEOF,
					nil,
				},
			},
			wantN:   1,
			wantErr: true,
		},
		{
			desc: "retry with retriable error only once",
			retry: Retry{
				MaxRetry: 1,
			},
			f: &retrySpy{
				errs: []error{
					RetriableError{
						Err: errors.New("retriable error"),
					},
					RetriableError{
						Err: errors.New("retriable error"),
					},
					nil,
				},
			},
			wantN:   1,
			wantErr: true,
		},
		{
			desc: "retry with shorter deadlinein f",
			f: &retrySpy{
				errs: []error{
					context.DeadlineExceeded,
					status.Error(codes.DeadlineExceeded, "deadline exceeded"),
					nil,
				},
			},
			wantN: 3,
		},
		{
			desc: "retry with RESOURCE_EXHAUSTED",
			f: &retrySpy{
				errs: []error{
					status.Error(codes.ResourceExhausted, "resource exhausted"),
					status.Error(codes.ResourceExhausted, "resource exhausted"),
					status.Error(codes.ResourceExhausted, "resource exhausted"),
					status.Error(codes.ResourceExhausted, "resource exhausted"),
					status.Error(codes.ResourceExhausted, "resource exhausted"),
					status.Error(codes.ResourceExhausted, "resource exhausted"),
					status.Error(codes.ResourceExhausted, "resource exhausted"),
					status.Error(codes.ResourceExhausted, "resource exhausted"),
					status.Error(codes.ResourceExhausted, "resource exhausted"),
					status.Error(codes.ResourceExhausted, "resource exhausted"),
				},
			},
			wantN:   5,
			wantErr: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			ctx := context.Background()
			err := tc.retry.Do(ctx, tc.f.f)
			if tc.f.n != tc.wantN || (err != nil) != tc.wantErr {
				t.Errorf("retry %d, %v; want %d, err=%t", tc.f.n, err, tc.wantN, tc.wantErr)
			}
		})
	}
}

func TestRetryDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	n := 0
	err := Retry{}.Do(ctx, func() error {
		n++
		<-ctx.Done()
		return ctx.Err()
	})
	if n > 1 || err == nil {
		t.Errorf("retry %d, %v; want 1, err", n, err)
	}
}
