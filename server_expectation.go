package grpcmock

import (
	"context"
	"sync"
	"time"

	"github.com/spf13/afero"
	"google.golang.org/grpc/codes"

	xmatcher "go.nhat.io/grpcmock/matcher"
	"go.nhat.io/grpcmock/service"
)

type baseExpectation struct {
	locker sync.Locker
	fs     afero.Fs
	delay  delayer

	serviceDesc *service.Method

	// requestHeader is a list of expected headers of the given request.
	requestHeader xmatcher.HeaderMatcher
	// requestPayload is the expected parameters of the given request.
	requestPayload *xmatcher.PayloadMatcher

	// statusCode is the response code when the request is handled.
	statusCode codes.Code //nolint: structcheck
	// statusMessage is the error message in case of failure.
	statusMessage string //nolint: structcheck

	fulfilledTimes uint
	repeatTimes    uint
}

func (e *baseExpectation) lock() {
	e.locker.Lock()
}

func (e *baseExpectation) unlock() {
	e.locker.Unlock()
}

// nolint: unused
func (e *baseExpectation) withFs(fs afero.Fs) {
	e.lock()
	defer e.unlock()

	e.fs = fs
}

func (e *baseExpectation) withTimes(t uint) {
	e.lock()
	defer e.unlock()

	e.repeatTimes = t
}

func (e *baseExpectation) ServiceMethod() service.Method {
	return *e.serviceDesc
}

func (e *baseExpectation) HeaderMatcher() xmatcher.HeaderMatcher {
	e.lock()
	defer e.unlock()

	return e.requestHeader
}

func (e *baseExpectation) PayloadMatcher() *xmatcher.PayloadMatcher {
	return e.requestPayload
}

func (e *baseExpectation) RemainTimes() uint {
	e.lock()
	defer e.unlock()

	return e.repeatTimes
}

func (e *baseExpectation) Fulfilled() {
	e.lock()
	defer e.unlock()

	if e.repeatTimes > 0 {
		e.repeatTimes--
	}

	e.fulfilledTimes++
}

func (e *baseExpectation) FulfilledTimes() uint {
	e.lock()
	defer e.unlock()

	return e.fulfilledTimes
}

type delayer func(ctx context.Context) error

func noWait() delayer {
	return func(ctx context.Context) error {
		return ctx.Err()
	}
}

func waitForSignal(c <-chan time.Time) delayer {
	return func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-c:
			return nil
		}
	}
}

func waitForDuration(d time.Duration) delayer {
	return func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-time.After(d):
			return nil
		}
	}
}
