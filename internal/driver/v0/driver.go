package v0

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
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"

	driverv1 "github.com/Xelon-AG/xelon-csi/internal/driver"
)

// Config is used to configure a new Driver
type Config struct {
	BaseURL        string
	ClientID       string
	Endpoint       string
	Mode           driverv1.Mode
	MetadataFile   string
	RescanOnResize bool
	Token          string
}

// DriverV0 implements the following CSI interfaces:
//   - csi.ControllerServer
//   - csi.NodeServer
//   - csi.IdentityServer
type DriverV0 struct {
	*controllerService
	*nodeService

	config *Config

	srv *grpc.Server
	log *logrus.Entry
}

// NewDriverV0 returns a configured CSI Xelon plugin.
func NewDriverV0(config *Config, log *logrus.Entry) (*DriverV0, error) {
	log.Infof("Initializing legacy Xelon Persistent Storage CSI Driver, built: %s, git state: %s", GetVersion().BuildDate, GetVersion().GitTreeState)

	d := &DriverV0{config: config}
	d.log = log

	switch config.Mode {
	case driverv1.ControllerMode:
		err := d.initializeControllerService(config)
		if err != nil {
			d.log.Errorf("couldn't initialize Xelon controller service, %s", err)
			return nil, err
		}
	case driverv1.NodeMode:
		err := d.newNodeService(config)
		if err != nil {
			d.log.Errorf("couldn't initialize Xelon node service, %s", err)
			return nil, err
		}
	case driverv1.AllMode:
		err := d.initializeControllerService(config)
		if err != nil {
			d.log.Errorf("couldn't initialize Xelon controller service, %s", err)
			return nil, err
		}

		err = d.newNodeService(config)
		if err != nil {
			d.log.Errorf("couldn't initialize Xelon node service, %s", err)
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown mode for driver: %s", config.Mode)
	}
	return d, nil
}

// Run starts the CSI Xelon plugin on the given endpoint.
func (d *DriverV0) Run() error {
	endpointURL, err := url.Parse(d.config.Endpoint)
	if err != nil {
		return err
	}

	if endpointURL.Scheme != "unix" {
		d.log.Errorf("only unix domain sockets are supported, not %s", endpointURL.Scheme)
		return errSchemeNotSupported
	}

	addr := path.Join(endpointURL.Host, filepath.FromSlash(endpointURL.Path))

	d.log.WithField("socket", addr).Info("removing existing socket file if existing")
	if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
		d.log.Errorf("failed to removed existing socket, %s", err)
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
			d.log.WithError(err).WithField("method", info.FullMethod).Error("method failed")
		}
		return resp, err
	}

	d.srv = grpc.NewServer(grpc.UnaryInterceptor(logErrorHandler))

	csi.RegisterIdentityServer(d.srv, d)

	switch d.config.Mode {
	case driverv1.ControllerMode:
		csi.RegisterControllerServer(d.srv, d)
	case driverv1.NodeMode:
		csi.RegisterNodeServer(d.srv, d)
	case driverv1.AllMode:
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
		d.log.Info("server stopped")
		d.srv.GracefulStop()
	}()

	d.log.WithField("grpc_addr", d.srv).Infof("starting server on %s", d.config.Endpoint)
	return d.srv.Serve(listener)
}
