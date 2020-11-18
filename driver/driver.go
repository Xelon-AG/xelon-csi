package driver

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
	"k8s.io/klog"
)

const (
	DefaultDriverName = "csi.xelon.ch"

	ControllerMode Mode = "controller"
	NodeMode       Mode = "node"
	AllMode        Mode = "all"
)

// Mode represents the mode in which the CSI driver started
type Mode string

// Config is used to configure a new Driver
type Config struct {
	BaseURL  string
	Endpoint string
	Mode     Mode
	Token    string
}

// Driver implements the following CSI interfaces:
//   - csi.ControllerServer
//   - csi.NodeServer
//   - csi.IdentityServer
type Driver struct {
	*controllerService
	*nodeService

	config *Config

	srv *grpc.Server
}

// NewDriver returns a configured CSI Xelon plugin.
func NewDriver(config *Config) (*Driver, error) {
	klog.Infof("Driver: %s, Version: %s", DefaultDriverName, driverVersion)

	d := &Driver{config: config}

	switch config.Mode {
	case ControllerMode:
		controllerService, err := newControllerService(config)
		if err != nil {
			klog.Errorf("couldn't initialize Xelon controller service, %s", err)
			return nil, err
		}
		d.controllerService = controllerService
	case NodeMode:
		d.nodeService = newNodeService()
	case AllMode:
		controllerService, err := newControllerService(config)
		if err != nil {
			klog.Errorf("couldn't initialize Xelon controller service, %s", err)
			return nil, err
		}
		d.controllerService = controllerService
		d.nodeService = newNodeService()
	default:
		return nil, fmt.Errorf("unknown mode for driver: %s", config.Mode)
	}

	return d, nil
}

// Run starts the CSI Xelon plugin on the given endpoint.
func (d *Driver) Run() error {
	endpointURL, err := url.Parse(d.config.Endpoint)
	if err != nil {
		return err
	}

	if endpointURL.Scheme != "unix" {
		klog.Errorf("only unix domain sockets are supported, not %s", endpointURL.Scheme)
		return errSchemeNotSupported
	}

	addr := path.Join(endpointURL.Host, filepath.FromSlash(endpointURL.Path))

	klog.Infof("Removing existing socket file if existing")
	if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
		klog.Errorf("failed to removed existing socket, %s", err)
		return errRemovingExistingSocket
	}

	dir := filepath.Dir(addr)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, os.ModePerm)
		if err != nil {
			return err
		}
	}

	listener, err := net.Listen(endpointURL.Scheme, addr)
	if err != nil {
		return err
	}

	// log response errors through a grpc unary interceptor
	logErrorHandler := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			klog.Errorf("error for %s: %v", info.FullMethod, err)
		}
		return resp, err
	}

	d.srv = grpc.NewServer(grpc.UnaryInterceptor(logErrorHandler))

	csi.RegisterIdentityServer(d.srv, d)

	switch d.config.Mode {
	case ControllerMode:
		csi.RegisterControllerServer(d.srv, d)
	case NodeMode:
		csi.RegisterNodeServer(d.srv, d)
	case AllMode:
		csi.RegisterControllerServer(d.srv, d)
		csi.RegisterNodeServer(d.srv, d)
	default:
		return fmt.Errorf("unknown mode for driver: %s", d.config.Mode)
	}

	// graceful shutdown
	gracefulStop := make(chan os.Signal, 1)
	signal.Notify(gracefulStop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-gracefulStop
		d.srv.GracefulStop()
	}()

	klog.Infof("Xelon CSI server started on %s", d.config.Endpoint)
	return d.srv.Serve(listener)
}
