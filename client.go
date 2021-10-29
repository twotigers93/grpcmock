package grpcmock

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"reflect"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

// ContextDialer is to set up the dialer.
type ContextDialer = func(context.Context, string) (net.Conn, error)

type invokeConfig struct {
	header   map[string]string
	dialOpts []grpc.DialOption
	callOpts []grpc.CallOption
}

// InvokeOption sets invoker config.
type InvokeOption func(c *invokeConfig)

// InvokeUnary invokes a unary method.
func InvokeUnary(
	ctx context.Context,
	method string,
	in interface{},
	out interface{},
	opts ...InvokeOption,
) error {
	ctx, conn, method, callOpts, err := prepInvoke(ctx, method, opts...)
	if err != nil {
		return err
	}

	return conn.Invoke(ctx, method, in, out, callOpts...)
}

// InvokeServerStream invokes a server-stream method.
func InvokeServerStream(
	ctx context.Context,
	method string,
	in interface{},
	handle func(stream grpc.ClientStream) error,
	opts ...InvokeOption,
) error {
	ctx, conn, method, callOpts, err := prepInvoke(ctx, method, opts...)
	if err != nil {
		return err
	}

	desc := &grpc.StreamDesc{ServerStreams: true}

	stream, err := conn.NewStream(ctx, desc, method, callOpts...)
	if err != nil {
		return err
	}

	if err := stream.SendMsg(in); err != nil {
		return err
	}

	if err := stream.CloseSend(); err != nil {
		return err
	}

	if handle == nil {
		return nil
	}

	return handle(stream)
}

func prepInvoke(ctx context.Context, method string, opts ...InvokeOption) (context.Context, *grpc.ClientConn, string, []grpc.CallOption, error) {
	addr, method, err := parseMethod(method)
	if err != nil {
		return ctx, nil, "", nil, fmt.Errorf("coulld not parse method url: %w", err)
	}

	ctx, dialOpts, callOpts := invokeOptions(ctx, opts...)

	conn, err := grpc.DialContext(ctx, addr, dialOpts...)
	if err != nil {
		return ctx, nil, "", nil, err
	}

	return ctx, conn, method, callOpts, err
}

func parseMethod(method string) (string, string, error) {
	u, err := url.Parse(method)
	if err != nil {
		return "", "", err
	}

	method = fmt.Sprintf("/%s", strings.TrimLeft(u.Path, "/"))

	if method == "/" {
		return "", "", ErrMissingMethod
	}

	addr := url.URL{
		Scheme: u.Scheme,
		User:   u.User,
		Host:   u.Host,
	}

	return addr.String(), method, nil
}

func invokeOptions(ctx context.Context, opts ...InvokeOption) (context.Context, []grpc.DialOption, []grpc.CallOption) {
	cfg := invokeConfig{
		header: map[string]string{},
	}

	for _, o := range opts {
		o(&cfg)
	}

	if len(cfg.header) > 0 {
		ctx = metadata.NewOutgoingContext(ctx, metadata.New(cfg.header))
	}

	return ctx, cfg.dialOpts, cfg.callOpts
}

// WithHeader sets request header.
func WithHeader(key, value string) InvokeOption {
	return func(c *invokeConfig) {
		c.header[key] = value
	}
}

// WithHeaders sets request header.
func WithHeaders(header map[string]string) InvokeOption {
	return func(c *invokeConfig) {
		for k, v := range header {
			c.header[k] = v
		}
	}
}

// WithContextDialer sets a context dialer to create connections.
//
// See:
// 	- grpcmock.WithBufConnDialer()
func WithContextDialer(d ContextDialer) InvokeOption {
	return WithDialOptions(grpc.WithContextDialer(d))
}

// WithBufConnDialer sets a *bufconn.Listener as the context dialer.
//
// See:
// 	- grpcmock.WithContextDialer()
func WithBufConnDialer(l *bufconn.Listener) InvokeOption {
	return WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return l.Dial()
	})
}

// WithInsecure disables transport security for the connections.
func WithInsecure() InvokeOption {
	return WithDialOptions(grpc.WithInsecure())
}

// WithDialOptions sets dial options.
func WithDialOptions(opts ...grpc.DialOption) InvokeOption {
	return func(c *invokeConfig) {
		c.dialOpts = append(c.dialOpts, opts...)
	}
}

// WithCallOption sets call options.
func WithCallOption(opts ...grpc.CallOption) InvokeOption {
	return func(c *invokeConfig) {
		c.callOpts = append(c.callOpts, opts...)
	}
}

// RecvAll reads everything from the stream and put into the output.
func RecvAll(out interface{}) func(stream grpc.ClientStream) error {
	return func(stream grpc.ClientStream) error {
		typeOfPtr := reflect.TypeOf(out)

		if typeOfPtr == nil || typeOfPtr.Kind() != reflect.Ptr {
			return fmt.Errorf("%T is not a pointer", out) // nolint: goerr113
		}

		typeOfSlice := typeOfPtr.Elem()

		if typeOfSlice.Kind() != reflect.Slice {
			return fmt.Errorf("%T is not a slice", out) // nolint: goerr113
		}

		typeOfMsg := typeOfSlice.Elem()
		newValueOf := reflect.MakeSlice(typeOfSlice, 0, 0)

		for {
			msg := newMessageValue(typeOfMsg)
			err := stream.RecvMsg(msg.Interface())

			if errors.Is(err, io.EOF) {
				break
			}

			if err != nil {
				return fmt.Errorf("could not recv msg: %w", err)
			}

			newValueOf = appendMessage(newValueOf, msg.Elem())
		}

		reflect.ValueOf(out).Elem().Set(newValueOf)

		return nil
	}
}

func newMessageValue(t reflect.Type) reflect.Value {
	if t.Kind() == reflect.Ptr {
		return newMessageValue(t.Elem())
	}

	return reflect.New(t)
}

func newSliceMessageValue(t reflect.Type, v reflect.Value) reflect.Value {
	if t.Kind() != reflect.Ptr {
		return v
	}

	result := reflect.New(t.Elem())

	result.Elem().Set(newSliceMessageValue(t.Elem(), v))

	return result
}

func appendMessage(s reflect.Value, v reflect.Value) reflect.Value {
	return reflect.Append(s, newSliceMessageValue(s.Type().Elem(), v))
}
