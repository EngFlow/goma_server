// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package rpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes"
	"go.opencensus.io/trace"
	epb "google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.chromium.org/goma/server/log"
)

// see google.golang.org/grpc backoff.go

// Retry handles rpc retry.
type Retry struct {
	// MaxRetry represents how many times to retry.
	// If it is not positive, it retries while error is
	// codes.Unavailable or codes.ResourceExhausted.
	MaxRetry  int
	BaseDelay time.Duration
	MaxDelay  time.Duration
}

func (r Retry) retry() int {
	if r.MaxRetry == 0 {
		return -1
	}
	return r.MaxRetry
}

func (r Retry) baseDelay() time.Duration {
	if r.BaseDelay == 0 && r.MaxDelay == 0 {
		return 10 * time.Millisecond
	}
	return r.BaseDelay
}

func (r Retry) maxDelay() time.Duration {
	if r.MaxDelay == 0 {
		return 120 * time.Second // ctx timeout if set?
	}
	return r.MaxDelay
}

func (r Retry) factor() float64 { return 1.6 }
func (r Retry) jitter() float64 { return 0.2 }

func (r Retry) backoff(n int) time.Duration {
	if n == 0 {
		return r.baseDelay()
	}
	backoff, max := float64(r.baseDelay()), float64(r.maxDelay())
	for backoff < max && n > 0 {
		backoff *= r.factor()
		n--
	}
	if backoff > max {
		backoff = max
	}
	backoff *= 1 + r.jitter()*(rand.Float64()*2-1)
	if backoff < 0 {
		return 0
	}
	return time.Duration(backoff)
}

// RetriableError represents retriable error in Retry.Do.
type RetriableError struct {
	Err   error
	Delay time.Duration
}

func (e RetriableError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("retriable error %v delay: %s", e.Err, e.Delay)
}

const (
	transportUnexpectedContentType     = `transport: received the unexpected content-type "text/html; charset=UTF-8"`
	streamTerminatedByRSTInternalError = `stream terminated by RST_STREAM with error code: INTERNAL_ERROR`
)

func retryInfo(ctx context.Context, err error) error {
	if e, ok := err.(RetriableError); ok {
		return e
	}
	if err == context.DeadlineExceeded && ctx.Err() == nil {
		// f might used shorter deadline than ctx for Retry.Do.
		// In this case, we could retry until ctx for Retry.Do
		// reaches deadline.
		return RetriableError{
			Err: err,
		}
	}
	st, ok := status.FromError(err)
	if !ok {
		return nil
	}
	switch st.Code() {
	case codes.Unavailable, codes.ResourceExhausted:

	case codes.DeadlineExceeded:
		if ctx.Err() == nil {
			// f might used shorter deadline than ctx for Retry.Do.
			// In this case, we could retry until ctx for Retry.Do
			// reaches deadline.
			return RetriableError{
				Err: err,
			}
		}

	case codes.Internal:
		// grpc sometimes gets internal error of
		//   transport: received the unexpected content-type "text/html; charset=UTF-8"
		// from cloud load balancer or endpoint-runtime (?).
		// http://b/122702746
		// to mitigate the issue, retry with such error.
		// it is not good to test error message, but there is no way
		// to distinguish with other internal error.
		if strings.Contains(st.Message(), transportUnexpectedContentType) {
			return RetriableError{
				Err: errors.New(st.Message()),
			}
		}
		// grpc also gets internal error of
		//  unexpected EOF
		// or
		//  stream terminated by RST_STREAM with error code: INTERNAL_ERROR
		// http://b/122995912
		if strings.Contains(st.Message(), io.ErrUnexpectedEOF.Error()) {
			return RetriableError{
				Err: errors.New(st.Message()),
			}
		}
		if strings.Contains(st.Message(), streamTerminatedByRSTInternalError) {
			return RetriableError{
				Err: errors.New(st.Message()),
			}
		}
		// other internal error is not retriable.
		return nil
	default:
		return nil
	}
	for _, d := range st.Details() {
		if ri, ok := d.(*epb.RetryInfo); ok {
			dur := ri.GetRetryDelay()
			d, err := ptypes.Duration(dur)
			return RetriableError{
				Err:   err,
				Delay: d,
			}
		}
	}
	return RetriableError{
		Err:   errors.New("no errdetails.RetryInfo in status"),
		Delay: 0,
	}
}

// Do calls f with retry, while f returns RetriableError, codes.Unavailable or
// codes.ResourceExhausted.
// It returns codes.DeadlineExceeded if ctx is cancelled.
// It returns last error if f returns error other than codes.Unavailable or
// codes.ResourceExhausted, or it reaches too many retries.
// It respects RetriableError.Delay or errdetail RetryInfo
// if it is specified as error details.
func (r Retry) Do(ctx context.Context, f func() error) error {
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/rpc.Retry.Do")
	defer span.End()
	logger := log.FromContext(ctx)

	var d []time.Duration
	var errs []error

	toError := func(errs []error) error {
		if len(errs) == 0 {
			return status.Errorf(codes.Internal, "retry finished with no error?")
		}
		if len(errs) == 1 {
			return errs[0]
		}
		lastErr, ok := status.FromError(errs[len(errs)-1])
		if !ok {
			return status.Errorf(codes.Unknown, "%v", errs)
		}
		return status.Errorf(lastErr.Code(), "%s :%v", lastErr.Message(), errs[:len(errs)-1])
	}

	for i := 0; ; i++ {
		if maxRetry := r.retry(); maxRetry > 0 && i >= maxRetry {
			span.Annotatef([]trace.Attribute{
				trace.Int64Attribute("retry", int64(i)),
			}, "too many retries %v: %v", d, errs)
			logger.Warnf("too many retries %d: %v: %v", i, d, errs)
			break
		}
		t := time.Now()
		err := f()
		if err == nil {
			return nil
		}
		d = append(d, time.Since(t))
		errs = append(errs, err)

		rerr, ok := retryInfo(ctx, err).(RetriableError)
		if !ok {
			return toError(errs)
		}
		if rerr.Err != nil {
			span.Annotatef(nil, "retryInfo.err: %v", rerr.Err)
		}
		if rerr.Delay > r.BaseDelay {
			r.BaseDelay = rerr.Delay
			span.Annotatef(nil, "retryInfo.delay=%s", rerr.Delay)
		}

		delay := r.backoff(i)
		span.Annotatef([]trace.Attribute{
			trace.Int64Attribute("retry", int64(i)),
			trace.Int64Attribute("backoff_msec", delay.Nanoseconds()/1e6),
		}, "retry backoff %s for err:%v", delay, err)
		logger.Warnf("retry %d backoff %s for err:%v", i, delay, err)
		select {
		case <-ctx.Done():
			return grpc.Errorf(codes.DeadlineExceeded, "%v: %v: %v", ctx.Err(), d, errs)
		case <-time.After(delay):
			d = append(d, delay)
		}
	}
	return toError(errs)
}
