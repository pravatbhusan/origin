package policy

import (
	"strings"
	"testing"

	"errors"

	buildapi "github.com/openshift/origin/pkg/build/api"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kapi "k8s.io/kubernetes/pkg/api"
)

type fakeBuildClient struct {
	builds         *buildapi.BuildList
	updateErrCount int
}

func newTestClient(builds []buildapi.Build) *fakeBuildClient {
	return &fakeBuildClient{builds: &buildapi.BuildList{Items: builds}}
}

func (f *fakeBuildClient) List(namespace string, opts metav1.ListOptions) (*buildapi.BuildList, error) {
	return f.builds, nil
}

func (f *fakeBuildClient) Update(namespace string, build *buildapi.Build) error {
	// Make sure every update fails at least once with conflict to ensure build updates are
	// retried.
	if f.updateErrCount == 0 {
		f.updateErrCount = 1
		return kerrors.NewConflict(kapi.Resource("builds"), build.Name, errors.New("confict"))
	} else {
		f.updateErrCount = 0
	}
	for i, item := range f.builds.Items {
		if build.Name == item.Name {
			f.builds.Items[i] = *build
		}
	}
	return nil
}

func addBuild(name, bcName string, phase buildapi.BuildPhase, policy buildapi.BuildRunPolicy) buildapi.Build {
	parts := strings.Split(name, "-")
	return buildapi.Build{
		Spec: buildapi.BuildSpec{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test",
			Labels: map[string]string{
				buildapi.BuildRunPolicyLabel: string(policy),
				buildapi.BuildConfigLabel:    bcName,
			},
			Annotations: map[string]string{
				buildapi.BuildNumberAnnotation: parts[len(parts)-1],
			},
		},
		Status: buildapi.BuildStatus{Phase: phase},
	}
}

func TestForBuild(t *testing.T) {
	builds := []buildapi.Build{
		addBuild("build-1", "sample-bc", buildapi.BuildPhaseNew, buildapi.BuildRunPolicyParallel),
		addBuild("build-2", "sample-bc", buildapi.BuildPhaseNew, buildapi.BuildRunPolicySerial),
		addBuild("build-3", "sample-bc", buildapi.BuildPhaseNew, buildapi.BuildRunPolicySerialLatestOnly),
	}
	client := newTestClient(builds)
	policies := GetAllRunPolicies(client, client)

	if policy := ForBuild(&builds[0], policies); policy != nil {
		if _, ok := policy.(*ParallelPolicy); !ok {
			t.Errorf("expected Parallel policy for build-1, got %T", policy)
		}
	} else {
		t.Errorf("expected Parallel policy for build-1, got nil")
	}

	if policy := ForBuild(&builds[1], policies); policy != nil {
		if _, ok := policy.(*SerialPolicy); !ok {
			t.Errorf("expected Serial policy for build-2, got %T", policy)
		}
	} else {
		t.Errorf("expected Serial policy for build-2, got nil")
	}

	if policy := ForBuild(&builds[2], policies); policy != nil {
		if _, ok := policy.(*SerialLatestOnlyPolicy); !ok {
			t.Errorf("expected SerialLatestOnly policy for build-3, got %T", policy)
		}
	} else {
		t.Errorf("expected SerialLatestOnly policy for build-3, got nil")
	}
}

func TestHandleCompleteSerial(t *testing.T) {
	builds := []buildapi.Build{
		addBuild("build-1", "sample-bc", buildapi.BuildPhaseComplete, buildapi.BuildRunPolicySerial),
		addBuild("build-2", "sample-bc", buildapi.BuildPhaseNew, buildapi.BuildRunPolicySerial),
		addBuild("build-3", "sample-bc", buildapi.BuildPhaseNew, buildapi.BuildRunPolicySerial),
	}

	client := newTestClient(builds)

	if err := handleComplete(client, client, &builds[0]); err != nil {
		t.Errorf("unexpected error %v", err)
	}

	resultBuilds, err := client.List("test", metav1.ListOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if _, ok := resultBuilds.Items[1].Annotations[buildapi.BuildAcceptedAnnotation]; !ok {
		t.Errorf("build-2 should have Annotation %s set to trigger it", buildapi.BuildAcceptedAnnotation)
	}

	if _, ok := resultBuilds.Items[2].Annotations[buildapi.BuildAcceptedAnnotation]; ok {
		t.Errorf("build-3 should not have Annotation %s set", buildapi.BuildAcceptedAnnotation)
	}
}

func TestHandleCompleteParallel(t *testing.T) {
	builds := []buildapi.Build{
		addBuild("build-1", "sample-bc", buildapi.BuildPhaseComplete, buildapi.BuildRunPolicyParallel),
		addBuild("build-2", "sample-bc", buildapi.BuildPhaseNew, buildapi.BuildRunPolicyParallel),
		addBuild("build-3", "sample-bc", buildapi.BuildPhaseNew, buildapi.BuildRunPolicyParallel),
	}

	client := newTestClient(builds)

	if err := handleComplete(client, client, &builds[0]); err != nil {
		t.Errorf("unexpected error %v", err)
	}

	resultBuilds, err := client.List("test", metav1.ListOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if _, ok := resultBuilds.Items[1].Annotations[buildapi.BuildAcceptedAnnotation]; !ok {
		t.Errorf("build-2 should have Annotation %s set to trigger it", buildapi.BuildAcceptedAnnotation)
	}

	if _, ok := resultBuilds.Items[2].Annotations[buildapi.BuildAcceptedAnnotation]; !ok {
		t.Errorf("build-3 should have Annotation %s set to trigger it", buildapi.BuildAcceptedAnnotation)
	}
}
