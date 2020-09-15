// Copyright 2020, OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package collection

import (
	"fmt"
	"strings"
	"testing"

	metricspb "github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1"
	resourcepb "github.com/census-instrumentation/opencensus-proto/gen-go/resource/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/testutils"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/utils"
)

func TestPodAndContainerMetrics(t *testing.T) {
	pod := newPodWithContainer(
		"1",
		podSpecWithContainer("container-name"),
		podStatusWithContainer("container-name", containerIDWithPreifx("container-id")),
	)
	dc := NewDataCollector(zap.NewNop(), []string{})

	dc.SyncMetrics(pod)
	actualResourceMetrics := dc.metricsStore.metricsCache

	rms := actualResourceMetrics["test-pod-1-uid"]
	require.NotNil(t, rms)

	rm := rms[0]
	require.Equal(t, 1, len(rm.Metrics))
	testutils.AssertResource(t, rm.Resource, k8sType,
		map[string]string{
			"k8s.pod.uid":        "test-pod-1-uid",
			"k8s.pod.name":       "test-pod-1",
			"k8s.node.name":      "test-node",
			"k8s.namespace.name": "test-namespace",
			"k8s.cluster.name":   "test-cluster",
		},
	)

	testutils.AssertMetrics(t, rm.Metrics[0], "k8s/pod/phase",
		metricspb.MetricDescriptor_GAUGE_INT64, 3)

	rm = rms[1]

	require.Equal(t, 4, len(rm.Metrics))
	testutils.AssertResource(t, rm.Resource, "container",
		map[string]string{
			"container.id":         "container-id",
			"k8s.container.name":   "container-name",
			"container.image.name": "container-image-name",
			"k8s.pod.uid":          "test-pod-1-uid",
			"k8s.pod.name":         "test-pod-1",
			"k8s.node.name":        "test-node",
			"k8s.namespace.name":   "test-namespace",
			"k8s.cluster.name":     "test-cluster",
		},
	)

	testutils.AssertMetrics(t, rm.Metrics[0], "k8s/container/restarts",
		metricspb.MetricDescriptor_GAUGE_INT64, 3)

	testutils.AssertMetrics(t, rm.Metrics[1], "k8s/container/ready",
		metricspb.MetricDescriptor_GAUGE_INT64, 1)

	testutils.AssertMetrics(t, rm.Metrics[2], "k8s/container/cpu/request",
		metricspb.MetricDescriptor_GAUGE_INT64, 10000)

	testutils.AssertMetrics(t, rm.Metrics[3], "k8s/container/cpu/limit",
		metricspb.MetricDescriptor_GAUGE_INT64, 20000)
}

func newPodWithContainer(id string, spec *corev1.PodSpec, status *corev1.PodStatus) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: v1.ObjectMeta{
			Name:        "test-pod-" + id,
			Namespace:   "test-namespace",
			UID:         types.UID("test-pod-" + id + "-uid"),
			ClusterName: "test-cluster",
			Labels: map[string]string{
				"foo":  "bar",
				"foo1": "",
			},
		},
		Spec:   *spec,
		Status: *status,
	}
}

var podSpecWithContainer = func(containerName string) *corev1.PodSpec {
	return &corev1.PodSpec{
		NodeName: "test-node",
		Containers: []corev1.Container{
			{
				Name:  containerName,
				Image: "container-image-name",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: *resource.NewQuantity(20, resource.DecimalSI),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU: *resource.NewQuantity(10, resource.DecimalSI),
					},
				},
			},
		},
	}
}

var podStatusWithContainer = func(containerName, containerID string) *corev1.PodStatus {
	return &corev1.PodStatus{
		Phase: corev1.PodSucceeded,
		ContainerStatuses: []corev1.ContainerStatus{
			{
				Name:         containerName,
				Ready:        true,
				RestartCount: 3,
				Image:        "container-image-name",
				ContainerID:  containerID,
				State: corev1.ContainerState{
					Running: &corev1.ContainerStateRunning{},
				},
			},
		},
	}
}

var containerIDWithPreifx = func(containerID string) string {
	return "docker://" + containerID
}

func TestListResourceMetrics(t *testing.T) {
	rms := map[string]*resourceMetrics{
		"resource-1": {resource: &resourcepb.Resource{Type: "type-1"}},
		"resource-2": {resource: &resourcepb.Resource{Type: "type-2"}},
		"resource-3": {resource: &resourcepb.Resource{Type: "type-1"}},
	}

	actual := listResourceMetrics(rms)
	expected := []*resourceMetrics{
		{resource: &resourcepb.Resource{Type: "type-1"}},
		{resource: &resourcepb.Resource{Type: "type-2"}},
		{resource: &resourcepb.Resource{Type: "type-1"}},
	}

	require.ElementsMatch(t, expected, actual)
}

func TestPhaseToInt(t *testing.T) {
	tests := []struct {
		name  string
		phase corev1.PodPhase
		want  int32
	}{
		{
			name:  "Pod phase pending",
			phase: corev1.PodPending,
			want:  1,
		},
		{
			name:  "Pod phase running",
			phase: corev1.PodRunning,
			want:  2,
		},
		{
			name:  "Pod phase succeeded",
			phase: corev1.PodSucceeded,
			want:  3,
		},
		{
			name:  "Pod phase failed",
			phase: corev1.PodFailed,
			want:  4,
		},
		{
			name:  "Pod phase unknown",
			phase: corev1.PodUnknown,
			want:  5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := phaseToInt(tt.phase); got != tt.want {
				t.Errorf("phaseToInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

var commonPodMetadata = map[string]string{
	"foo":                    "bar",
	"foo1":                   "",
	"pod.creation_timestamp": "0001-01-01T00:00:00Z",
}

var allPodMetadata = func(metadata map[string]string) map[string]string {
	out := utils.MergeStringMaps(metadata, commonPodMetadata)
	return out
}

func TestDataCollectorSyncMetadata(t *testing.T) {
	tests := []struct {
		name          string
		metadataStore *metadataStore
		resource      interface{}
		want          map[ResourceID]*KubernetesMetadata
	}{
		{
			name:          "Pod and container metadata simple case",
			metadataStore: &metadataStore{},
			resource: newPodWithContainer(
				"0",
				podSpecWithContainer("container-name"),
				podStatusWithContainer("container-name", "container-id"),
			),
			want: map[ResourceID]*KubernetesMetadata{
				ResourceID("test-pod-0-uid"): {
					resourceIDKey: "k8s.pod.uid",
					resourceID:    "test-pod-0-uid",
					metadata:      commonPodMetadata,
				},
				ResourceID("container-id"): {
					resourceIDKey: "container.id",
					resourceID:    "container-id",
					metadata: map[string]string{
						"container.status": "running",
					},
				},
			},
		},
		{
			name:          "Empty container id skips container resource",
			metadataStore: &metadataStore{},
			resource: newPodWithContainer(
				"0",
				podSpecWithContainer("container-name"),
				podStatusWithContainer("container-name", ""),
			),
			want: map[ResourceID]*KubernetesMetadata{
				ResourceID("test-pod-0-uid"): {
					resourceIDKey: "k8s.pod.uid",
					resourceID:    "test-pod-0-uid",
					metadata:      commonPodMetadata,
				},
			},
		},
		{
			name:          "Pod with Owner Reference",
			metadataStore: &metadataStore{},
			resource: withOwnerReferences([]v1.OwnerReference{
				{
					Kind: "StatefulSet",
					Name: "test-statefulset-0",
					UID:  "test-statefulset-0-uid",
				},
			}, newPodWithContainer("0", &corev1.PodSpec{}, &corev1.PodStatus{})),
			want: map[ResourceID]*KubernetesMetadata{
				ResourceID("test-pod-0-uid"): {
					resourceIDKey: "k8s.pod.uid",
					resourceID:    "test-pod-0-uid",
					metadata: allPodMetadata(map[string]string{
						"k8s.workload.kind": "StatefulSet",
						"k8s.workload.name": "test-statefulset-0",
						"statefulset":       "test-statefulset-0",
						"statefulset_uid":   "test-statefulset-0-uid",
					}),
				},
			},
		},
		{
			name: "Pod with Service metadata",
			metadataStore: &metadataStore{
				services: &testutils.MockStore{
					Cache: map[string]interface{}{
						"test-namespace/test-service": &corev1.Service{
							ObjectMeta: v1.ObjectMeta{
								Name:      "test-service",
								Namespace: "test-namespace",
								UID:       "test-service-uid",
							},
							Spec: corev1.ServiceSpec{
								Selector: map[string]string{
									"k8s-app": "my-app",
								},
							},
						},
					},
				},
			},
			resource: podWithAdditionalLabels(
				map[string]string{"k8s-app": "my-app"},
				newPodWithContainer("0", &corev1.PodSpec{}, &corev1.PodStatus{}),
			),
			want: map[ResourceID]*KubernetesMetadata{
				ResourceID("test-pod-0-uid"): {
					resourceIDKey: "k8s.pod.uid",
					resourceID:    "test-pod-0-uid",
					metadata: allPodMetadata(map[string]string{
						"kubernetes_service_test-service": "",
						"k8s-app":                         "my-app",
					}),
				},
			},
		},
	}

	for _, tt := range tests {
		observedLogger, _ := observer.New(zapcore.WarnLevel)
		logger := zap.New(observedLogger)
		t.Run(tt.name, func(t *testing.T) {
			dc := &DataCollector{
				logger:                 logger,
				metadataStore:          tt.metadataStore,
				nodeConditionsToReport: []string{},
			}

			actual := dc.SyncMetadata(tt.resource)
			require.Equal(t, len(tt.want), len(actual))

			for key, item := range tt.want {
				got, exists := actual[key]
				require.True(t, exists)
				require.Equal(t, *item, *got)
			}
		})
	}
}

func TestDataCollectorSyncMetadataForPodWorkloads(t *testing.T) {
	tests := []struct {
		name             string
		withParentOR     bool
		emptyCache       bool
		wantNilCache     bool
		wantErrFromCache bool
		logMessage       string
	}{
		{
			name: "Pod metadata with Owner reference",
		},
		{
			name:         "Pod metadata with parent Owner reference",
			withParentOR: true,
		},
		{
			name:         "Owner reference - cache nil",
			wantNilCache: true,
		},
		{
			name:       "Owner reference - does not exist in cache",
			emptyCache: true,
			logMessage: "Resource does not exist in store, properties from it will not be synced.",
		},
		{
			name:             "Owner reference - cache error",
			wantErrFromCache: true,
			logMessage:       "Failed to get resource from store, properties from it will not be synced.",
		},
	}

	for _, kind := range []string{"Job", "ReplicaSet"} {
		for _, tt := range tests {
			testCase := testCaseForPodWorkload(testCaseOptions{
				kind:             kind,
				withParentOR:     tt.withParentOR,
				emptyCache:       tt.emptyCache,
				wantNilCache:     tt.wantNilCache,
				wantErrFromCache: tt.wantErrFromCache,
				logMessage:       tt.logMessage,
			})

			// Ensure required mockups are available.
			require.NotNil(t, testCase.metadataStore)
			require.NotNil(t, testCase.resource)

			observedLogger, logs := observer.New(zapcore.WarnLevel)
			logger := zap.New(observedLogger)

			name := fmt.Sprintf("(%s) - %s", kind, tt.name)
			t.Run(name, func(t *testing.T) {
				dc := &DataCollector{
					logger:                 logger,
					metadataStore:          testCase.metadataStore,
					nodeConditionsToReport: []string{},
				}

				actual := dc.SyncMetadata(testCase.resource)
				require.Equal(t, len(testCase.want), len(actual))

				for key, item := range testCase.want {
					got, exists := actual[key]
					require.True(t, exists)

					for k, v := range commonPodMetadata {
						item.metadata[k] = v
					}
					require.Equal(t, *item, *got)

					if testCase.logMessage != "" {
						require.GreaterOrEqual(t, 1, logs.Len())
						require.Equal(t, testCase.logMessage, logs.All()[0].Message)
					}
				}
			})
		}
	}
}

type testCase struct {
	metadataStore *metadataStore
	resource      interface{}
	want          map[ResourceID]*KubernetesMetadata
	logMessage    string
}

type testCaseOptions struct {
	kind             string
	withParentOR     bool
	emptyCache       bool
	wantNilCache     bool
	wantErrFromCache bool
	logMessage       string
}

func testCaseForPodWorkload(to testCaseOptions) testCase {
	out := testCase{
		metadataStore: mockMetadataStore(to),
		resource:      podWithOwnerReference(to.kind),
		want:          expectedKubernetesMetadata(to),
		logMessage:    to.logMessage,
	}
	return out
}

func expectedKubernetesMetadata(to testCaseOptions) map[ResourceID]*KubernetesMetadata {
	podUIDLabel := "test-pod-0-uid"
	kindLower := strings.ToLower(to.kind)
	kindObjName := fmt.Sprintf("test-%s-0", kindLower)
	kindObjUID := fmt.Sprintf("test-%s-0-uid", kindLower)
	kindNameLabel := fmt.Sprintf("k8s.%s.name", kindLower)

	out := map[ResourceID]*KubernetesMetadata{
		ResourceID(podUIDLabel): {
			resourceIDKey: "k8s.pod.uid",
			resourceID:    ResourceID(podUIDLabel),
			metadata: map[string]string{
				kindLower:          kindObjName,
				kindLower + "_uid": kindObjUID,
			},
		},
	}

	withoutInfoFromCache := to.emptyCache || to.wantNilCache || to.wantErrFromCache

	// Add metadata gathered from informer caches to expected metadata.
	if !withoutInfoFromCache {
		out[ResourceID(podUIDLabel)].metadata["k8s.workload.kind"] = to.kind
		out[ResourceID(podUIDLabel)].metadata["k8s.workload.name"] = kindObjName
		out[ResourceID(podUIDLabel)].metadata[kindNameLabel] = kindObjName
	}

	// If the Pod's Owner Kind is not the actual owner (CronJobs -> Jobs and Deployments -> ReplicaSets),
	// add metadata additional metadata to expected values.
	if to.withParentOR {
		delete(out[ResourceID(podUIDLabel)].metadata, kindNameLabel)
		switch to.kind {
		case "Job":
			out[ResourceID(podUIDLabel)].metadata["cronjob_uid"] = "test-cronjob-0-uid"
			out[ResourceID(podUIDLabel)].metadata["k8s.cronjob.name"] = "test-cronjob-0"
			out[ResourceID(podUIDLabel)].metadata["k8s.workload.name"] = "test-cronjob-0"
			out[ResourceID(podUIDLabel)].metadata["k8s.workload.kind"] = "CronJob"
		case "ReplicaSet":
			out[ResourceID(podUIDLabel)].metadata["deployment_uid"] = "test-deployment-0-uid"
			out[ResourceID(podUIDLabel)].metadata["k8s.deployment.name"] = "test-deployment-0"
			out[ResourceID(podUIDLabel)].metadata["k8s.workload.name"] = "test-deployment-0"
			out[ResourceID(podUIDLabel)].metadata["k8s.workload.kind"] = "Deployment"
		}
	}
	return out
}
