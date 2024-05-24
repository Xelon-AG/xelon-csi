package xelon

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"syscall"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
)

type DriverOption func(*Driver)

// Mode represents the mode in which the CSI driver started
type Mode string

const (
	DefaultDriverName = "csi.xelon.ch"

	ControllerMode Mode = "controller"
	NodeMode       Mode = "node"
	AllMode        Mode = "all"
)

var (
	_ csi.ControllerServer = &Driver{}
	_ csi.IdentityServer   = &Driver{}
	_ csi.NodeServer       = &Driver{}
)

// Driver implements the following CSI interfaces:
//   - csi.ControllerServer
//   - csi.NodeServer
//   - csi.IdentityServer
type Driver struct {
	*controllerService
	*nodeService
	srv *grpc.Server

	endpoint string
	mode     Mode
	opts     *Options
}

func NewDriver(ctx context.Context, opts *Options) (*Driver, error) {
	klog.InfoS("Driver information", "driver", DefaultDriverName, "version", "dev")

	d := &Driver{
		endpoint: opts.Endpoint,
		mode:     opts.Mode,
		opts:     opts,
	}

	switch d.mode {
	case ControllerMode:
		if controllerService, err := newControllerService(ctx, opts); err != nil {
			return nil, err
		} else {
			d.controllerService = controllerService
		}
	case NodeMode:
		if nodeService, err := newNodeService(ctx); err != nil {
			return nil, err
		} else {
			d.nodeService = nodeService
		}
	case AllMode:
		if controllerService, err := newControllerService(ctx, opts); err != nil {
			return nil, err
		} else {
			d.controllerService = controllerService
		}
		if nodeService, err := newNodeService(ctx); err != nil {
			return nil, err
		} else {
			d.nodeService = nodeService
		}
	default:
		return nil, fmt.Errorf("unknown mode for driver: %s", d.mode)
	}

	return d, nil
}

func (d *Driver) Run() error {
	endpointURL, err := url.Parse(d.endpoint)
	if err != nil {
		return err
	}
	grpcAddr := path.Join(endpointURL.Host, filepath.FromSlash(endpointURL.Path))
	if endpointURL.Host == "" {
		grpcAddr = filepath.FromSlash(endpointURL.Path)
	}
	if endpointURL.Scheme != "unix" {
		return fmt.Errorf("only unix domain sockets are supported, not %s", endpointURL.Scheme)
	}

	// remove the socket if it's already there
	if err := os.Remove(grpcAddr); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing socket %s, error: %s", grpcAddr, err)
	}

	grpcListener, err := net.Listen(endpointURL.Scheme, grpcAddr)
	if err != nil {
		return err
	}

	// log response errors through a grpc unary interceptor
	logErrorHandler := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			klog.ErrorS(err, "GRPC error")
		}
		return resp, err
	}
	d.srv = grpc.NewServer(grpc.UnaryInterceptor(logErrorHandler))

	csi.RegisterIdentityServer(d.srv, d)

	switch d.mode {
	case ControllerMode:
		csi.RegisterControllerServer(d.srv, d)
	case NodeMode:
		csi.RegisterNodeServer(d.srv, d)
	case AllMode:
		csi.RegisterControllerServer(d.srv, d)
		csi.RegisterNodeServer(d.srv, d)
	default:
		return fmt.Errorf("unknown mode for driver: %s", d.mode)
	}

	// graceful shutdown
	gracefulStop := make(chan os.Signal, 1)
	signal.Notify(gracefulStop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-gracefulStop
		klog.InfoS("Stopping GRPC server gracefully", "endpoint", d.endpoint)
		d.srv.GracefulStop()
	}()

	klog.InfoS("Starting GRPC server", "endpoint", d.endpoint)
	return d.srv.Serve(grpcListener)
}
