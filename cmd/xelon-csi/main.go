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
	driverv0 "github.com/Xelon-AG/xelon-csi/internal/driver/v0"
)

// command line flags
var (
	endpoint        = flag.String("endpoint", "unix:///var/lib/kubelet/plugins/csi.xelon.ch/csi.sock", "CSI endpoint")
	mode            = flag.String("mode", string(driverv1.AllMode), "The mode in which the CSI driver will be run (all, node, controller)")
	rescanOnResize  = flag.Bool("rescan-on-resize", true, "Rescan block device and verify its size before expanding the filesystem (node mode)")
	useLegacyDriver = flag.Bool("use-legacy-driver", false, "Run Xelon CSI driver in legacy mode")
	xelonBaseURL    = flag.String("xelon-base-url", "https://vdc.xelon.ch/api/service/", "Xelon API URL")
	xelonClientID   = flag.String("xelon-client-id", "", "Xelon client ID for IP ranges")
	xelonCloudID    = flag.String("xelon-cloud-id", "", "Xelon client ID for IP ranges")
	xelonToken      = flag.String("xelon-token", "", "Xelon access token")

	// v0 flags for compatibility
	logLevel     = flag.String("log-level", "info", "The log level for the CSI driver (deprecated)")
	metadataFile = flag.String("metadata-file", "/etc/init.d/metadata.json", "The path to the metadata file on Xelon devices (deprecated)")
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

	// handling legacy driver mode
	if *useLegacyDriver {
		klog.V(0).InfoS("Starting legacy Xelon CSI driver. This mode will be deprecated soon.")
		logger := driverv0.InitializeLogging(*logLevel, *mode, *metadataFile)
		d, err := driverv0.NewDriverV0(
			&driverv0.Config{
				BaseURL:        *xelonBaseURL,
				ClientID:       *xelonClientID,
				Endpoint:       *endpoint,
				Mode:           driverv1.Mode(*mode),
				MetadataFile:   *metadataFile,
				RescanOnResize: *rescanOnResize,
				Token:          *xelonToken,
			},
			logger)
		if err != nil {
			logger.Fatalln(err)
		}

		if err := d.Run(); err != nil {
			logger.Fatalln(err)
		}
		return
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
