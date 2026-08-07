package main

import (
	"context"
	"flag"
	"os"
	"time"

	"k8s.io/component-base/featuregate"
	"k8s.io/component-base/logs"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/component-base/logs/json"
	"k8s.io/klog/v2"

	driverv1 "github.com/Xelon-AG/xelon-csi/internal/driver"
)

// command line flags
var (
	endpoint       = flag.String("endpoint", "unix:///var/lib/kubelet/plugins/csi.xelon.ch/csi.sock", "CSI endpoint")
	mode           = flag.String("mode", string(driverv1.AllMode), "The mode in which the CSI driver will be run (all, node, controller)")
	rescanOnResize = flag.Bool("rescan-on-resize", true, "Rescan block device and verify its size before expanding the filesystem (node mode)")
	xelonBaseURL   = flag.String("xelon-base-url", "https://vdc.xelon.ch/api/service/", "Xelon API URL")
	xelonClientID  = flag.String("xelon-client-id", "", "Xelon client ID for IP ranges")
	xelonCloudID   = flag.String("xelon-cloud-id", "", "Xelon client ID for IP ranges")
	xelonToken     = flag.String("xelon-token", "", "Xelon access token")
)

func main() {
	// logging configuration
	fg := featuregate.NewFeatureGate()
	if err := logsapi.RegisterLogFormat(logsapi.JSONLogFormat, json.Factory{}, logsapi.LoggingBetaOptions); err != nil {
		klog.ErrorS(err, "Failed to register JSON log format")
	}
	c := logsapi.NewLoggingConfiguration()
	if err := logsapi.AddFeatureGates(fg); err != nil {
		klog.ErrorS(err, "Failed to add feature gates")
	}
	logsapi.AddGoFlags(c, flag.CommandLine)
	flag.Parse()
	defer logs.FlushLogs()

	if err := logsapi.ValidateAndApply(c, fg); err != nil {
		klog.ErrorS(err, "Failed to validate and apply logging configuration")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	d, err := driverv1.NewDriver(
		ctx,
		&driverv1.Options{
			Endpoint:       *endpoint,
			Mode:           driverv1.Mode(*mode),
			RescanOnResize: *rescanOnResize,
			XelonBaseURL:   *xelonBaseURL,
			XelonClientID:  *xelonClientID,
			XelonCloudID:   *xelonCloudID,
			XelonToken:     *xelonToken,
		},
	)
	if err != nil {
		klog.ErrorS(err, "Failed to initialize driver")
		os.Exit(255)
	}

	if err := d.Run(); err != nil {
		klog.ErrorS(err, "Could not run driver")
		os.Exit(255)
	}
}
