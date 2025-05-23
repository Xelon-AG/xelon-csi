package cloud

import (
	"context"
	"errors"
	"fmt"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

const (
	LabelXelonLocalVMIDDeprecated = "kubernetes.xelon.ch/localvmid"
	LabelXelonLocalVMID           = "node.kubernetes.io/localvmid"
)

// Metadata is info about the Xelon Device on which driver is running
type Metadata struct {
	LocalVMID string
	Name      string
}

func RetrieveMetadata(ctx context.Context) (*Metadata, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	k8sClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	nodeName := os.Getenv("CSI_NODE_NAME")
	if nodeName == "" {
		return nil, errors.New("CSI_NODE_NAME environment variable must be set")
	}

	metadata := &Metadata{Name: nodeName}

	node, err := k8sClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error getting Node %v: %w", nodeName, err)
	}
	if localVMID, ok := node.GetLabels()[LabelXelonLocalVMID]; ok {
		metadata.LocalVMID = localVMID
	}
	// fallback to verify old label format
	if metadata.LocalVMID == "" {
		if localVMID, ok := node.GetLabels()[LabelXelonLocalVMIDDeprecated]; ok {
			klog.V(2).InfoS("Fallback to get localVMID via deprecated label",
				"label", LabelXelonLocalVMIDDeprecated,
				"localvmid", localVMID,
			)
			metadata.LocalVMID = localVMID
		}
	}

	return metadata, nil
}
