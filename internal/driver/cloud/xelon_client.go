package cloud

import (
	"errors"

	"github.com/Xelon-AG/xelon-sdk-go/xelon"
)

type ClientOptions xelon.ClientOption

func NewXelonClient(token, clientID, baseURL string) (*xelon.Client, error) {
	if token == "" {
		return nil, errors.New("token must not be empty")
	}
	if clientID == "" {
		return nil, errors.New("client id must not be empty")
	}
	if baseURL == "" {
		return nil, errors.New("base url must not be empty")
	}

	var opts []xelon.ClientOption
	opts = append(opts, xelon.WithBaseURL(baseURL))
	opts = append(opts, xelon.WithClientID(clientID))
	opts = append(opts, xelon.WithUserAgent("xelon-csi"))

	client := xelon.NewClient(token, opts...)
	return client, nil
}
