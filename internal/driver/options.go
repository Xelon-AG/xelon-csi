package driver

// Options contains parsed CLI flags passed to the driver.
type Options struct {
	Endpoint       string
	Mode           Mode
	RescanOnResize bool
	XelonBaseURL   string
	XelonClientID  string
	XelonCloudID   string
	XelonToken     string
}
