package driver

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sync"

	"github.com/Xelon-AG/xelon-sdk-go/xelon"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"k8s.io/klog"
)

const DefaultDriverName = "csi.xelon.ch"

type Driver struct {
	name string

	isController bool

	srv *grpc.Server

	client   *xelon.Client
	tenantID string

	mux sync.Mutex
}

func NewDriver(token, driverName string, controller bool) (*Driver, error) {
	if driverName == "" {
		driverName = DefaultDriverName
	}

	client := xelon.NewClient(token)
	client.SetBaseURL("https://vdcnew.xelon.ch/api/service/")
	client.SetUserAgent("xelon-csi/dev")

	tenant, _, err := client.Tenant.Get(context.Background())
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize Xelon client: %s", err)
	}

	return &Driver{
		name:         driverName,
		isController: controller,

		client:   client,
		tenantID: tenant.TenantID,
	}, nil
}

func (d *Driver) Run(ctx context.Context) error {
	// url-socket
	u, _ := url.Parse("unix:///var/lib/kubelet/plugins/csi.xelon.ch/csi.sock")
	grpcAddr := path.Join(u.Host, filepath.FromSlash(u.Path))
	if u.Host == "" {
		grpcAddr = filepath.FromSlash(u.Path)
	}
	if err := os.Remove(grpcAddr); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove unix domain socket file %s, error: %s", grpcAddr, err)
	}

	grpcListener, err := net.Listen(u.Scheme, grpcAddr)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}

	// log response errors for better observability
	errHandler := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			klog.Errorf("error for %s: %v", info.FullMethod, err)
		}
		return resp, err
	}
	d.srv = grpc.NewServer(grpc.UnaryInterceptor(errHandler))
	csi.RegisterIdentityServer(d.srv, d)
	csi.RegisterControllerServer(d.srv, d)
	csi.RegisterNodeServer(d.srv, d)

	var eg errgroup.Group
	eg.Go(func() error {
		go func() {
			<-ctx.Done()
			klog.Info("server stopped")
			d.mux.Lock()
			d.mux.Unlock()
			d.srv.GracefulStop()
		}()
		return d.srv.Serve(grpcListener)
	})

	return eg.Wait()
}
