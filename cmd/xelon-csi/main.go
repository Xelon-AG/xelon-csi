package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/Xelon-AG/xelon-csi/driver"
)

func main() {
	var (
		apiURL       = flag.String("api-url", "https://vdc.xelon.ch/api/service/", "Xelon API URL")
		endpoint     = flag.String("endpoint", "unix:///var/lib/kubelet/plugins/"+driver.DefaultDriverName+"/csi.sock", "CSI endpoint")
		mode         = flag.String("mode", string(driver.AllMode), "The mode in which the CSI driver will be run (all, node, controller)")
		metadataFile = flag.String("metadata-file", "/etc/init.d/metadata.json", "Path to the metadata file on CSI nodes")
		token        = flag.String("token", "", "Xelon access token")
		version      = flag.Bool("version", false, "Print the version and exit.")
	)
	flag.Parse()

	if *version {
		info := driver.GetVersion()
		fmt.Println("Xelon Persistent Storage CSI Driver")
		fmt.Printf(" Version:      %s\n", info.DriverVersion)
		fmt.Printf(" Built:        %s\n", info.BuildDate)
		fmt.Printf(" Git commit:   %s\n", info.GitCommit)
		fmt.Printf(" Git state:    %s\n", info.GitTreeState)
		fmt.Printf(" Go version:   %s\n", info.GoVersion)
		fmt.Printf(" OS/Arch:      %s\n", info.Platform)
		os.Exit(0)
	}

	d, err := driver.NewDriver(&driver.Config{
		BaseURL:      *apiURL,
		Endpoint:     *endpoint,
		Mode:         driver.Mode(*mode),
		MetadataFile: *metadataFile,
		Token:        *token,
	})
	if err != nil {
		log.Fatalln(err)
	}

	if err := d.Run(); err != nil {
		log.Fatalln(err)
	}
}
