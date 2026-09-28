package driver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Xelon-AG/xelon-sdk-go/xelon"
)

const (
	testCloudID  = "cloud-identifier"
	testTenantID = "tenant-identifier"
)

type expectedAPIRequest struct {
	method       string
	path         string
	rawQuery     string
	body         string
	responseBody string
	statusCode   int
}

type scriptedAPI struct {
	t            *testing.T
	mu           sync.Mutex
	expectations []expectedAPIRequest
	next         int
}

func newScriptedAPIServer(t *testing.T, expectations ...expectedAPIRequest) *httptest.Server {
	t.Helper()

	api := &scriptedAPI{t: t, expectations: expectations}
	server := httptest.NewServer(api)
	t.Cleanup(func() {
		server.Close()
		api.mu.Lock()
		defer api.mu.Unlock()
		if api.next != len(api.expectations) {
			t.Errorf("received %d API requests, expected %d", api.next, len(api.expectations))
		}
	})

	return server
}

func (a *scriptedAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.next >= len(a.expectations) {
		a.t.Errorf("unexpected API request: %s %s", r.Method, r.URL.RequestURI())
		http.Error(w, "unexpected request", http.StatusInternalServerError)
		return
	}
	expected := a.expectations[a.next]
	a.next++

	if r.Method != expected.method {
		a.t.Errorf("request %d method: got %q, want %q", a.next, r.Method, expected.method)
	}
	if r.URL.Path != expected.path {
		a.t.Errorf("request %d path: got %q, want %q", a.next, r.URL.Path, expected.path)
	}
	if r.URL.RawQuery != expected.rawQuery {
		a.t.Errorf("request %d query: got %q, want %q", a.next, r.URL.RawQuery, expected.rawQuery)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
		a.t.Errorf("request %d authorization: got %q", a.next, got)
	}
	if got := r.Header.Get("X-User-Id"); got != "test-client" {
		a.t.Errorf("request %d client ID: got %q", a.next, got)
	}
	a.assertRequestBody(r, expected.body)

	w.Header().Set("Content-Type", "application/json")
	statusCode := expected.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	w.WriteHeader(statusCode)
	if expected.responseBody != "" {
		if _, err := io.WriteString(w, expected.responseBody); err != nil {
			a.t.Errorf("request %d response: %v", a.next, err)
		}
	}
}

func (a *scriptedAPI) assertRequestBody(r *http.Request, expectedBody string) {
	a.t.Helper()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		a.t.Errorf("request %d body: %v", a.next, err)
		return
	}
	if expectedBody == "" {
		if len(body) != 0 {
			a.t.Errorf("request %d body: got %q, want empty body", a.next, body)
		}
		return
	}

	var got any
	if err := json.Unmarshal(body, &got); err != nil {
		a.t.Errorf("request %d body is not valid JSON: %v", a.next, err)
		return
	}
	var want any
	if err := json.Unmarshal([]byte(expectedBody), &want); err != nil {
		a.t.Errorf("invalid expected JSON for request %d: %v", a.next, err)
		return
	}
	if !reflect.DeepEqual(got, want) {
		a.t.Errorf("request %d body: got %s, want %s", a.next, body, expectedBody)
	}
}

func newTestDriver(server *httptest.Server) *Driver {
	client := xelon.NewClient(
		"test-token",
		xelon.WithBaseURL(server.URL+"/api/v2/"),
		xelon.WithClientID("test-client"),
		xelon.WithHTTPClient(server.Client()),
	)

	return &Driver{controllerService: &controllerService{
		xelon:                     client,
		cloudID:                   testCloudID,
		tenantID:                  testTenantID,
		volumeStatusCheckInterval: time.Millisecond,
		volumeStatusCheckTimeout:  time.Second,
	}}
}

func validCreateVolumeRequest(name string) *csi.CreateVolumeRequest {
	return &csi.CreateVolumeRequest{
		Name:               name,
		VolumeCapabilities: []*csi.VolumeCapability{{}},
	}
}

func TestNewControllerServiceUsesAPIv2IdentityEndpoints(t *testing.T) {
	server := newScriptedAPIServer(t,
		expectedAPIRequest{
			method:       http.MethodGet,
			path:         "/api/v2/tenants/current",
			responseBody: fmt.Sprintf(`{"identifier":%q}`, testTenantID),
		},
		expectedAPIRequest{
			method:       http.MethodGet,
			path:         "/api/v2/clouds",
			rawQuery:     "tenantIdentifier=" + testTenantID,
			responseBody: fmt.Sprintf(`[{"identifier":%q}]`, testCloudID),
		},
	)

	service, err := newControllerService(context.Background(), &Options{
		XelonBaseURL:  server.URL + "/api/v2/",
		XelonClientID: "test-client",
		XelonCloudID:  testCloudID,
		XelonToken:    "test-token",
	})
	if err != nil {
		t.Fatalf("newControllerService() error = %v", err)
	}
	if service.tenantID != testTenantID {
		t.Errorf("tenant ID = %q, want %q", service.tenantID, testTenantID)
	}
	if service.cloudID != testCloudID {
		t.Errorf("cloud ID = %q, want %q", service.cloudID, testCloudID)
	}
}

func TestCreateVolumeAPIv2Contract(t *testing.T) {
	const (
		volumeName = "pvc-exact"
		storageID  = "storage-identifier"
	)
	server := newScriptedAPIServer(t,
		expectedAPIRequest{
			method:       http.MethodGet,
			path:         "/api/v2/persistent-storages",
			rawQuery:     "search=" + volumeName,
			responseBody: `{"data":[{"identifier":"fuzzy-result","name":"pvc-exact-copy"}]}`,
		},
		expectedAPIRequest{
			method: http.MethodPost,
			path:   "/api/v2/persistent-storages",
			body: fmt.Sprintf(`{
				"cloudIdentifier": %q,
				"name": %q,
				"storageSize": 10,
				"tenantIdentifier": %q,
				"type": 2
			}`, testCloudID, volumeName, testTenantID),
			responseBody: fmt.Sprintf(`{"data":{"identifier":%q,"name":%q}}`, storageID, volumeName),
		},
		expectedAPIRequest{
			method:       http.MethodGet,
			path:         "/api/v2/persistent-storages/" + storageID,
			responseBody: fmt.Sprintf(`{"identifier":%q,"name":%q,"capacity":10,"uuid":"storage-uuid","formatted":true}`, storageID, volumeName),
		},
	)

	response, err := newTestDriver(server).CreateVolume(context.Background(), validCreateVolumeRequest(volumeName))
	if err != nil {
		t.Fatalf("CreateVolume() error = %v", err)
	}
	if got := response.GetVolume().GetVolumeId(); got != storageID {
		t.Errorf("volume ID = %q, want %q", got, storageID)
	}
}

func TestCreateVolumeReturnsExistingExactMatch(t *testing.T) {
	const volumeName = "pvc-exact"
	server := newScriptedAPIServer(t, expectedAPIRequest{
		method:   http.MethodGet,
		path:     "/api/v2/persistent-storages",
		rawQuery: "search=" + volumeName,
		responseBody: `{"data":[
			{"identifier":"fuzzy-result","name":"pvc-exact-copy","capacity":10,"uuid":"fuzzy-uuid","formatted":true},
			{"identifier":"existing-identifier","name":"pvc-exact","capacity":10,"uuid":"exact-uuid","formatted":true}
		]}`,
	})

	response, err := newTestDriver(server).CreateVolume(context.Background(), validCreateVolumeRequest(volumeName))
	if err != nil {
		t.Fatalf("CreateVolume() error = %v", err)
	}
	if got := response.GetVolume().GetVolumeId(); got != "existing-identifier" {
		t.Errorf("volume ID = %q, want %q", got, "existing-identifier")
	}
}

func TestCreateVolumeFailsOnMultipleExactMatches(t *testing.T) {
	const volumeName = "duplicate-name"
	server := newScriptedAPIServer(t, expectedAPIRequest{
		method:       http.MethodGet,
		path:         "/api/v2/persistent-storages",
		rawQuery:     "search=" + volumeName,
		responseBody: `{"data":[{"identifier":"first","name":"duplicate-name"},{"identifier":"second","name":"duplicate-name"}]}`,
	})

	_, err := newTestDriver(server).CreateVolume(context.Background(), validCreateVolumeRequest(volumeName))
	if status.Code(err) != codes.Internal {
		t.Fatalf("CreateVolume() code = %s, want %s (error: %v)", status.Code(err), codes.Internal, err)
	}
}

func TestDeleteVolumeAPIv2Contract(t *testing.T) {
	const volumeHandle = "existing-volume-handle"
	server := newScriptedAPIServer(t, expectedAPIRequest{
		method: http.MethodDelete,
		path:   "/api/v2/persistent-storages/" + volumeHandle,
	})

	_, err := newTestDriver(server).DeleteVolume(context.Background(), &csi.DeleteVolumeRequest{VolumeId: volumeHandle})
	if err != nil {
		t.Fatalf("DeleteVolume() error = %v", err)
	}
}

func TestControllerPublishVolumeAPIv2ContractPreservesVolumeHandle(t *testing.T) {
	const (
		volumeHandle = "existing-volume-handle"
		nodeID       = "existing-node-identifier"
	)
	server := newScriptedAPIServer(t,
		expectedAPIRequest{
			method:       http.MethodGet,
			path:         "/api/v2/persistent-storages/" + volumeHandle,
			responseBody: fmt.Sprintf(`{"identifier":%q,"name":"existing-volume","uuid":"storage-uuid","formatted":true}`, volumeHandle),
		},
		expectedAPIRequest{
			method:       http.MethodGet,
			path:         "/api/v2/devices/" + nodeID,
			responseBody: fmt.Sprintf(`{"identifier":%q,"displayName":"worker-1"}`, nodeID),
		},
		expectedAPIRequest{
			method:       http.MethodPost,
			path:         "/api/v2/persistent-storages/" + volumeHandle + "/attach-device",
			body:         fmt.Sprintf(`{"deviceIdentifier":%q}`, nodeID),
			responseBody: `{}`,
		},
	)

	response, err := newTestDriver(server).ControllerPublishVolume(context.Background(), &csi.ControllerPublishVolumeRequest{
		VolumeId:         volumeHandle,
		NodeId:           nodeID,
		VolumeCapability: &csi.VolumeCapability{},
	})
	if err != nil {
		t.Fatalf("ControllerPublishVolume() error = %v", err)
	}
	if got := response.PublishContext[xelonStorageUUID]; got != "storage-uuid" {
		t.Errorf("published storage UUID = %q, want %q", got, "storage-uuid")
	}
}

func TestControllerUnpublishVolumeAPIv2Contract(t *testing.T) {
	const (
		volumeHandle = "existing-volume-handle"
		nodeID       = "existing-node-identifier"
	)
	server := newScriptedAPIServer(t,
		expectedAPIRequest{
			method:       http.MethodGet,
			path:         "/api/v2/persistent-storages/" + volumeHandle,
			responseBody: fmt.Sprintf(`{"identifier":%q,"name":"existing-volume","uuid":"storage-uuid","formatted":true}`, volumeHandle),
		},
		expectedAPIRequest{
			method:       http.MethodGet,
			path:         "/api/v2/devices/" + nodeID,
			responseBody: fmt.Sprintf(`{"identifier":%q,"displayName":"worker-1"}`, nodeID),
		},
		expectedAPIRequest{
			method:       http.MethodPost,
			path:         "/api/v2/persistent-storages/" + volumeHandle + "/detach-device",
			body:         fmt.Sprintf(`{"deviceIdentifier":%q}`, nodeID),
			responseBody: `{}`,
		},
	)

	_, err := newTestDriver(server).ControllerUnpublishVolume(context.Background(), &csi.ControllerUnpublishVolumeRequest{
		VolumeId: volumeHandle,
		NodeId:   nodeID,
	})
	if err != nil {
		t.Fatalf("ControllerUnpublishVolume() error = %v", err)
	}
}

func TestControllerExpandVolumeAPIv2Contract(t *testing.T) {
	const volumeHandle = "existing-volume-handle"
	server := newScriptedAPIServer(t,
		expectedAPIRequest{
			method:       http.MethodGet,
			path:         "/api/v2/persistent-storages/" + volumeHandle,
			responseBody: fmt.Sprintf(`{"identifier":%q,"capacity":5,"uuid":"storage-uuid","formatted":true}`, volumeHandle),
		},
		expectedAPIRequest{
			method:       http.MethodPost,
			path:         "/api/v2/persistent-storages/" + volumeHandle + "/extend",
			body:         `{"diskSize":10}`,
			responseBody: `{}`,
		},
		expectedAPIRequest{
			method:       http.MethodGet,
			path:         "/api/v2/persistent-storages/" + volumeHandle,
			responseBody: fmt.Sprintf(`{"identifier":%q,"capacity":10,"uuid":"storage-uuid","formatted":true}`, volumeHandle),
		},
	)

	response, err := newTestDriver(server).ControllerExpandVolume(context.Background(), &csi.ControllerExpandVolumeRequest{
		VolumeId:      volumeHandle,
		CapacityRange: &csi.CapacityRange{RequiredBytes: 10 * giB},
	})
	if err != nil {
		t.Fatalf("ControllerExpandVolume() error = %v", err)
	}
	if response == nil {
		t.Fatal("ControllerExpandVolume() response = nil")
	}
	if response.CapacityBytes != 10*giB {
		t.Errorf("capacity bytes = %d, want %d", response.CapacityBytes, 10*giB)
	}
}

func TestCreateVolumeRejectsEmptyCreateResponse(t *testing.T) {
	server := newScriptedAPIServer(t,
		expectedAPIRequest{
			method:       http.MethodGet,
			path:         "/api/v2/persistent-storages",
			rawQuery:     "search=empty-response",
			responseBody: `{"data":[]}`,
		},
		expectedAPIRequest{
			method:       http.MethodPost,
			path:         "/api/v2/persistent-storages",
			body:         fmt.Sprintf(`{"cloudIdentifier":%q,"name":"empty-response","storageSize":10,"tenantIdentifier":%q,"type":2}`, testCloudID, testTenantID),
			responseBody: `{}`,
		},
	)

	_, err := newTestDriver(server).CreateVolume(context.Background(), validCreateVolumeRequest("empty-response"))
	if status.Code(err) != codes.Internal {
		t.Fatalf("CreateVolume() code = %s, want %s (error: %v)", status.Code(err), codes.Internal, err)
	}
}

func TestControllerPublishVolumeRejectsMalformedStorageResponse(t *testing.T) {
	server := newScriptedAPIServer(t, expectedAPIRequest{
		method:       http.MethodGet,
		path:         "/api/v2/persistent-storages/malformed-volume",
		responseBody: `{}`,
	})

	_, err := newTestDriver(server).ControllerPublishVolume(context.Background(), &csi.ControllerPublishVolumeRequest{
		VolumeId:         "malformed-volume",
		NodeId:           "node-identifier",
		VolumeCapability: &csi.VolumeCapability{},
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("ControllerPublishVolume() code = %s, want %s (error: %v)", status.Code(err), codes.Internal, err)
	}
}

func TestControllerPublishVolumeRejectsMalformedDeviceResponse(t *testing.T) {
	server := newScriptedAPIServer(t,
		expectedAPIRequest{
			method:       http.MethodGet,
			path:         "/api/v2/persistent-storages/existing-volume",
			responseBody: `{"identifier":"existing-volume","uuid":"storage-uuid","formatted":true}`,
		},
		expectedAPIRequest{
			method:       http.MethodGet,
			path:         "/api/v2/devices/malformed-device",
			responseBody: `{}`,
		},
	)

	_, err := newTestDriver(server).ControllerPublishVolume(context.Background(), &csi.ControllerPublishVolumeRequest{
		VolumeId:         "existing-volume",
		NodeId:           "malformed-device",
		VolumeCapability: &csi.VolumeCapability{},
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("ControllerPublishVolume() code = %s, want %s (error: %v)", status.Code(err), codes.Internal, err)
	}
}

func TestCreateVolumeStopsReadinessPollingWhenContextIsCanceled(t *testing.T) {
	server := newScriptedAPIServer(t,
		expectedAPIRequest{
			method:       http.MethodGet,
			path:         "/api/v2/persistent-storages",
			rawQuery:     "search=canceled-volume",
			responseBody: `{"data":[]}`,
		},
		expectedAPIRequest{
			method:       http.MethodPost,
			path:         "/api/v2/persistent-storages",
			body:         fmt.Sprintf(`{"cloudIdentifier":%q,"name":"canceled-volume","storageSize":10,"tenantIdentifier":%q,"type":2}`, testCloudID, testTenantID),
			responseBody: `{"data":{"identifier":"canceled-storage","name":"canceled-volume"}}`,
		},
	)
	driver := newTestDriver(server)
	driver.volumeStatusCheckInterval = time.Hour
	driver.volumeStatusCheckTimeout = time.Hour

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := driver.CreateVolume(ctx, validCreateVolumeRequest("canceled-volume"))
	if err == nil {
		t.Fatal("CreateVolume() error = nil, want readiness cancellation error")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("CreateVolume() took %v after context cancellation", elapsed)
	}
}
